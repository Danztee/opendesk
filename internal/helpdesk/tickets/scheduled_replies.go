package tickets

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/useopendesk/opendesk/internal/auth"
	"github.com/useopendesk/opendesk/internal/db"
	"github.com/useopendesk/opendesk/internal/deps"
	"github.com/useopendesk/opendesk/internal/domain"
	"github.com/useopendesk/opendesk/internal/helpdesk/hd"
	"github.com/useopendesk/opendesk/internal/httpx"
	"github.com/useopendesk/opendesk/internal/jobs"
)

// Scheduled replies (contracts/api.md "Reply drafts"): a member's reply draft can go out
// later. Scheduling keeps the reply as the send button gives it (recipients, address,
// status) with the draft, checks it as a send would, and a job sends it at send_at as that
// member, through PostMessage, so the message is stamped when it actually goes out. Until
// then the member can send it now, schedule it for another time or cancel, which keeps it
// as a draft. A scheduled send that is no longer allowed keeps the draft with the reason.

const (
	// scheduleMin and scheduleMax bound how far ahead a reply can be scheduled.
	scheduleMin = time.Minute
	scheduleMax = 365 * 24 * time.Hour
	// scheduledReplyAttempts bounds retries of a send that failed for a passing reason.
	scheduledReplyAttempts = 3
)

// ScheduledSend is when and how a scheduled reply draft goes out.
type ScheduledSend struct {
	SendAt    time.Time `json:"send_at"`
	Cc        []string  `json:"cc"`
	Bcc       []string  `json:"bcc"`
	Status    *string   `json:"status" doc:"The ticket's status after it is sent, if the reply sets one"`
	MailboxID string    `json:"mailbox_id" doc:"The address it goes out from, when the reply names one"`
}

// scheduleInput is POST /v1/tickets/{id}/reply-draft/schedule.
type scheduleInput struct {
	SendAt time.Time     `json:"send_at" doc:"When it goes out: at least a minute and at most a year from now"`
	Reply  *MessageInput `json:"reply,omitempty" doc:"The reply as the send button would post it (kind reply). Left out: the one already scheduled, at a new time"`
}

// storedReply is a scheduled reply without its body and files, which stay the draft's.
func storedReply(in MessageInput) MessageInput {
	in.BodyHTML, in.AttachmentIDs = "", nil
	return in
}

func (s ScheduledSend) from(in MessageInput) ScheduledSend {
	s.Cc, s.Bcc, s.Status, s.MailboxID = nonNil(in.Cc), nonNil(in.Bcc), in.Status, in.MailboxID
	return s
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func registerScheduledReplies(d *deps.Deps, api huma.API) {
	tags := []string{"Tickets"}
	type draftOut struct {
		Body struct {
			Draft *ReplyDraft `json:"draft"`
		}
	}
	member := func(ctx context.Context) (*auth.Principal, error) {
		p, err := auth.RequireWorkspace(ctx, auth.RoleAgent, auth.ScopeTicketsWrite)
		if err != nil {
			return nil, httpx.Wrap(err)
		}
		if p.ActorUserID() == nil || p.Assistant {
			return nil, httpx.E(http.StatusForbidden, "forbidden", "A scheduled reply belongs to a signed-in member.")
		}
		return p, nil
	}

	huma.Register(api, huma.Operation{OperationID: "schedule-reply", Method: http.MethodPost, Path: "/v1/tickets/{id}/reply-draft/schedule",
		Summary: "Schedule your reply", Tags: tags,
		Description: "Keeps the reply as your draft and sends it at send_at, as you, with the same checks as sending now. Without reply, moves the reply already scheduled to send_at."},
		func(ctx context.Context, in *struct {
			ID   string `path:"id"`
			Body scheduleInput
		}) (*draftOut, error) {
			p, err := member(ctx)
			if err != nil {
				return nil, err
			}
			in.Body.SendAt = in.Body.SendAt.Truncate(time.Microsecond)
			now := time.Now()
			if in.Body.SendAt.Before(now.Add(scheduleMin)) || in.Body.SendAt.After(now.Add(scheduleMax)) {
				return nil, httpx.Validation("send_at must be between a minute and a year from now")
			}
			out := &draftOut{}
			err = d.DB.Tx(ctx, func(tx pgx.Tx) error {
				tid, err := domain.ResolveVisibleTicket(ctx, tx, p, in.ID)
				if err != nil {
					return err
				}
				reply, err := scheduledReply(ctx, tx, p, tid, in.Body.Reply)
				if err != nil {
					return err
				}
				// Checked as a send would be, then undone: nothing goes out now.
				if err := dryRun(ctx, tx, d, p, tid, reply); err != nil {
					return err
				}
				attIDs, err := hd.ParseAttachmentIDs(reply.AttachmentIDs)
				if err != nil {
					return err
				}
				html := hd.SanitizeRich(reply.BodyHTML)
				stored, err := json.Marshal(storedReply(reply))
				if err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO ticket_reply_drafts (ticket_id, user_id, workspace_id, body_html, body_text, attachment_ids,
						send_at, send_request, send_error)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '')
					ON CONFLICT (ticket_id, user_id) DO UPDATE SET body_html = EXCLUDED.body_html, body_text = EXCLUDED.body_text,
						attachment_ids = EXCLUDED.attachment_ids, send_at = EXCLUDED.send_at, send_request = EXCLUDED.send_request,
						send_error = '', updated_at = now()`,
					tid, p.UserID, p.WorkspaceID, html, hd.HTMLToText(html), attIDs, in.Body.SendAt, stored); err != nil {
					return err
				}
				// A job at send_at; one for an earlier time finds the draft moved and does nothing.
				if _, err := d.Jobs.InsertTx(ctx, tx, jobs.ScheduledReplyArgs{WorkspaceID: p.WorkspaceID, TicketID: tid, UserID: p.UserID,
					SendAt: in.Body.SendAt.UTC()}, &river.InsertOpts{ScheduledAt: in.Body.SendAt, MaxAttempts: scheduledReplyAttempts}); err != nil {
					return err
				}
				out.Body.Draft, err = loadReplyDraft(ctx, tx, p.WorkspaceID, tid, p.UserID)
				return err
			})
			if err != nil {
				return nil, httpx.Wrap(err)
			}
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "unschedule-reply", Method: http.MethodDelete, Path: "/v1/tickets/{id}/reply-draft/schedule",
		Summary: "Cancel your scheduled reply", Description: "Nothing is sent; the reply stays as your draft.", Tags: tags},
		func(ctx context.Context, in *struct {
			ID string `path:"id"`
		}) (*draftOut, error) {
			p, err := member(ctx)
			if err != nil {
				return nil, err
			}
			out := &draftOut{}
			err = d.DB.Tx(ctx, func(tx pgx.Tx) error {
				tid, err := domain.ResolveVisibleTicket(ctx, tx, p, in.ID)
				if err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE ticket_reply_drafts SET send_at = NULL, send_error = '', updated_at = now()
					WHERE workspace_id = $1 AND ticket_id = $2 AND user_id = $3`, p.WorkspaceID, tid, p.UserID); err != nil {
					return err
				}
				out.Body.Draft, err = loadReplyDraft(ctx, tx, p.WorkspaceID, tid, p.UserID)
				return err
			})
			if err != nil {
				return nil, httpx.Wrap(err)
			}
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "send-scheduled-reply", Method: http.MethodPost, Path: "/v1/tickets/{id}/reply-draft/send",
		Summary: "Send your scheduled reply now", Tags: tags, DefaultStatus: http.StatusCreated},
		func(ctx context.Context, in *struct {
			ID string `path:"id"`
		}) (*struct{ Body domain.Message }, error) {
			p, err := member(ctx)
			if err != nil {
				return nil, err
			}
			var msg domain.Message
			err = d.DB.Tx(ctx, func(tx pgx.Tx) error {
				tid, err := domain.ResolveVisibleTicket(ctx, tx, p, in.ID)
				if err != nil {
					return err
				}
				reply, _, err := loadScheduled(ctx, tx, p.WorkspaceID, tid, p.UserID)
				if err != nil {
					return err
				}
				if reply == nil {
					return httpx.NotFound("Scheduled reply")
				}
				if msg, err = PostMessage(ctx, tx, d, p, tid, *reply); err != nil {
					return err
				}
				return dropScheduled(ctx, tx, p.WorkspaceID, tid, p.UserID)
			})
			if err != nil {
				return nil, httpx.Wrap(err)
			}
			return &struct{ Body domain.Message }{msg}, nil
		})
}

// scheduledReply is the reply to schedule: the one given, or the one already scheduled.
func scheduledReply(ctx context.Context, tx pgx.Tx, p *auth.Principal, tid uuid.UUID, given *MessageInput) (MessageInput, error) {
	if given == nil {
		reply, _, err := loadScheduled(ctx, tx, p.WorkspaceID, tid, p.UserID)
		if err != nil {
			return MessageInput{}, err
		}
		if reply == nil {
			return MessageInput{}, httpx.Validation("reply is required: nothing is scheduled on this ticket yet")
		}
		return *reply, nil
	}
	r := *given
	switch {
	case r.Kind != "reply":
		return r, httpx.Validation("only a reply can be scheduled")
	case r.Direction != "" && r.Direction != "outbound":
		return r, httpx.Validation("only a reply you send can be scheduled")
	case r.FromNoteID != "":
		return r, httpx.Validation("a reply written from a note is sent now")
	}
	return r, nil
}

// loadScheduled is the scheduled reply of a member's draft, whole again (body and files), and
// when it is due; nil when the draft is not scheduled.
func loadScheduled(ctx context.Context, q db.Querier, wid, tid, uid uuid.UUID) (*MessageInput, *time.Time, error) {
	var html string
	var ids []uuid.UUID
	var at *time.Time
	var raw []byte
	err := q.QueryRow(ctx, `SELECT body_html, attachment_ids, send_at, send_request FROM ticket_reply_drafts
		WHERE workspace_id = $1 AND ticket_id = $2 AND user_id = $3 FOR UPDATE`, wid, tid, uid).Scan(&html, &ids, &at, &raw)
	if db.IsNotFound(err) || (err == nil && (at == nil || raw == nil)) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var in MessageInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, nil, err
	}
	in.BodyHTML = html
	for _, id := range ids {
		in.AttachmentIDs = append(in.AttachmentIDs, id.String())
	}
	return &in, at, nil
}

// dropScheduled removes a scheduled reply draft once it went out (sending another reply
// leaves a scheduled one alone, domain.AddMessage).
func dropScheduled(ctx context.Context, tx pgx.Tx, wid, tid, uid uuid.UUID) error {
	_, err := tx.Exec(ctx, `DELETE FROM ticket_reply_drafts WHERE workspace_id = $1 AND ticket_id = $2 AND user_id = $3`, wid, tid, uid)
	return err
}

// dryRun checks a reply as sending it would, inside a savepoint it then rolls back.
func dryRun(ctx context.Context, tx pgx.Tx, d *deps.Deps, p *auth.Principal, tid uuid.UUID, reply MessageInput) error {
	if _, err := tx.Exec(ctx, `SAVEPOINT schedule_check`); err != nil {
		return err
	}
	_, err := PostMessage(ctx, tx, d, p, tid, reply)
	if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT schedule_check`); rerr != nil {
		return rerr
	}
	return err
}

// ScheduledReplyWorker sends a scheduled reply draft when it is due.
type ScheduledReplyWorker struct {
	river.WorkerDefaults[jobs.ScheduledReplyArgs]
	D *deps.Deps
}

func (w *ScheduledReplyWorker) Work(ctx context.Context, job *river.Job[jobs.ScheduledReplyArgs]) error {
	return SendScheduledReply(ctx, w.D, job.Args, job.Attempt >= job.MaxAttempts)
}

// SendScheduledReply sends the member's scheduled reply if it is still scheduled for a.SendAt.
// A reply that may not go out any more (the member left or lost the role, the ticket is gone
// or hidden from them, the address is no longer theirs) is kept as their draft with the
// reason; a passing failure is retried, and the last try keeps the draft too.
func SendScheduledReply(ctx context.Context, d *deps.Deps, a jobs.ScheduledReplyArgs, last bool) error {
	var retry error
	err := d.DB.Tx(ctx, func(tx pgx.Tx) error {
		reply, at, err := loadScheduled(ctx, tx, a.WorkspaceID, a.TicketID, a.UserID)
		if err != nil || reply == nil || !at.Equal(a.SendAt) {
			return err // sent, cancelled or moved since
		}
		keep := func(reason string) error {
			_, err := tx.Exec(ctx, `UPDATE ticket_reply_drafts SET send_at = NULL, send_error = $4, updated_at = now()
				WHERE workspace_id = $1 AND ticket_id = $2 AND user_id = $3`, a.WorkspaceID, a.TicketID, a.UserID, reason)
			return err
		}
		p, err := scheduledPrincipal(ctx, tx, a.WorkspaceID, a.UserID)
		if err != nil {
			return err
		}
		if p == nil {
			return keep("You can no longer reply on this ticket, so the reply was not sent.")
		}
		if _, err := tx.Exec(ctx, `SAVEPOINT scheduled_send`); err != nil {
			return err
		}
		if _, err = domain.ResolveVisibleTicket(ctx, tx, p, a.TicketID.String()); err == nil {
			_, err = PostMessage(ctx, tx, d, p, a.TicketID, *reply)
		}
		if err == nil {
			return dropScheduled(ctx, tx, a.WorkspaceID, a.TicketID, a.UserID)
		}
		if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT scheduled_send`); rerr != nil {
			return rerr
		}
		var he *httpx.Error
		switch {
		case errors.As(err, &he):
			return keep("The scheduled reply was not sent: " + he.Detail)
		case db.IsNotFound(err):
			return keep("The ticket is gone, so the reply was not sent.")
		case last:
			d.Log.Error("scheduled reply", "err", err, "ticket_id", a.TicketID)
			return keep("The scheduled reply could not be sent. Send it again.")
		}
		retry = err
		return nil
	})
	if err != nil {
		return err
	}
	return retry
}

// scheduledPrincipal is the member who scheduled a reply, as they would be signed in now, or
// nil when they may no longer reply: not a member or below agent, suspended, or the workspace
// suspended.
func scheduledPrincipal(ctx context.Context, q db.Querier, wid, uid uuid.UUID) (*auth.Principal, error) {
	var role string
	err := q.QueryRow(ctx, `SELECT m.role FROM members m JOIN users u ON u.id = m.user_id JOIN workspaces w ON w.id = m.workspace_id
		WHERE m.workspace_id = $1 AND m.user_id = $2 AND u.suspended_at IS NULL AND w.suspended_at IS NULL`, wid, uid).Scan(&role)
	if db.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := &auth.Principal{Kind: auth.KindUser, UserID: uid, WorkspaceID: wid, Role: auth.Role(role)}
	if !p.Role.AtLeast(auth.RoleAgent) {
		return nil, nil
	}
	return p, nil
}
