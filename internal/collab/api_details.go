package collab

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/useopendesk/opendesk/internal/auth"
	"github.com/useopendesk/opendesk/internal/httpx"
	"github.com/useopendesk/opendesk/internal/mailsource"
)

// messageSource reads a message's raw source for the caller: the message must be one they see.
func (s *service) messageSource(ctx context.Context, rawID string) ([]byte, *messageRow, error) {
	p, err := s.require(ctx, auth.RoleViewer)
	if err != nil {
		return nil, nil, httpx.Wrap(err)
	}
	id, err := parseID(rawID, "Message")
	if err != nil {
		return nil, nil, err
	}
	m, err := scanMessage(s.d.DB.Pool.QueryRow(ctx, `SELECT `+messageColumns+` FROM collab_messages m WHERE m.workspace_id = $1 AND m.id = $2`, p.WorkspaceID, id))
	if err != nil {
		return nil, nil, notFound(err, "Message")
	}
	t, err := visibleThread(ctx, s.d.DB.Pool, p, m.ThreadID)
	if err != nil {
		return nil, nil, notFound(err, "Message")
	}
	a, err := loadAccount(ctx, s.d.DB.Pool, p.WorkspaceID, t.AccountID)
	if err != nil {
		return nil, nil, notFound(err, "Message")
	}
	raw, err := s.loadRaw(ctx, a, m, t.RemoteID)
	if err != nil {
		s.log.Warn("collab message source", "message_id", m.ID, "err", err)
		return nil, nil, httpx.E(http.StatusBadGateway, "provider_error", "The message could not be read from the mail provider. Try again.")
	}
	if len(raw) > mailsource.MaxBytes {
		return nil, nil, httpx.E(http.StatusRequestEntityTooLarge, "too_large", "This message is too large to show its source.")
	}
	return raw, m, nil
}

func (s *service) registerDetailRoutes(api huma.API) {
	collabOp(api, huma.Operation{OperationID: "get-collab-message-source", Method: http.MethodGet, Path: "/v1/collab/messages/{id}/source",
		Summary: "Show a message's original source"},
		func(ctx context.Context, in *struct {
			ID string `path:"id"`
		}) (*struct{ Body mailsource.MailSource }, error) {
			raw, _, err := s.messageSource(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			return &struct{ Body mailsource.MailSource }{mailsource.View(raw)}, nil
		})
	collabOp(api, huma.Operation{OperationID: "download-collab-message-source", Method: http.MethodGet, Path: "/v1/collab/messages/{id}/source.eml",
		Summary: "Download a message's original source"},
		func(ctx context.Context, in *struct {
			ID string `path:"id"`
		}) (*mailsource.File, error) {
			raw, m, err := s.messageSource(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			return mailsource.Download(raw, m.Subject), nil
		})

	collabOp(api, huma.Operation{OperationID: "get-collab-message-details", Method: http.MethodGet, Path: "/v1/collab/messages/{id}/details",
		Summary: "How a message came: its sender checks and headers"},
		func(ctx context.Context, in *struct {
			ID string `path:"id"`
		}) (*struct{ Body MessageDetails }, error) {
			p, err := s.require(ctx, auth.RoleViewer)
			if err != nil {
				return nil, httpx.Wrap(err)
			}
			id, err := parseID(in.ID, "Message")
			if err != nil {
				return nil, err
			}
			m, err := scanMessage(s.d.DB.Pool.QueryRow(ctx, `SELECT `+messageColumns+` FROM collab_messages m WHERE m.workspace_id = $1 AND m.id = $2`,
				p.WorkspaceID, id))
			if err != nil {
				return nil, notFound(err, "Message")
			}
			t, err := visibleThread(ctx, s.d.DB.Pool, p, m.ThreadID)
			if err != nil {
				return nil, notFound(err, "Message")
			}
			a, err := loadAccount(ctx, s.d.DB.Pool, p.WorkspaceID, t.AccountID)
			if err != nil {
				return nil, notFound(err, "Message")
			}
			raw, err := s.loadRaw(ctx, a, m, t.RemoteID)
			if err != nil {
				s.log.Warn("collab message source", "message_id", m.ID, "err", err)
				return nil, httpx.E(http.StatusBadGateway, "provider_error", "The message could not be read from the mail provider. Try again.")
			}
			return &struct{ Body MessageDetails }{messageDetails(raw, a.Provider, m.From.Email, m.outgoing())}, nil
		})
}
