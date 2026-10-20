package tickets_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/useopendesk/opendesk/internal/auth"
	"github.com/useopendesk/opendesk/internal/domain"
	"github.com/useopendesk/opendesk/internal/helpdesk/hdtest"
)

func createInternalNote(t *testing.T, e *hdtest.Env, ws hdtest.WS, ticketID string) domain.Message {
	t.Helper()
	var note domain.Message
	e.Do("POST", "/v1/tickets/"+ticketID+"/messages", ws.Agent.As(ws),
		map[string]any{"kind": "note", "body_html": "<p>Original note</p>"}).Want(t, 201).JSON(t, &note)
	return note
}

func TestEditInternalNote(t *testing.T) {
	e, ws := newEnv(t)
	tk := createTicket(t, e, ws, map[string]any{"channel": "api", "kind": "note"})
	note := createInternalNote(t, e, ws, tk.ID.String())
	path := "/v1/tickets/" + fmt.Sprint(tk.Number) + "/messages/" + note.ID.String()
	_, seq := e.Events(ws.ID, 0)

	var edited domain.Message
	e.Do("PATCH", path, ws.Agent.As(ws), map[string]string{"body_html": "<p>Corrected note</p><script>alert(1)</script>"}).
		Want(t, 200).JSON(t, &edited)
	if edited.BodyText != "Corrected note" || edited.EditedAt == nil || strings.Contains(edited.BodyHTML, "script") {
		t.Fatalf("edited note %+v", edited)
	}
	var activityData string
	if err := e.D.DB.Pool.QueryRow(e.Ctx, `SELECT data::text FROM activities WHERE ticket_id = $1 AND type = 'note_edited'`, tk.ID).
		Scan(&activityData); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(activityData, note.ID.String()) || strings.Contains(activityData, "Corrected note") {
		t.Fatalf("activity data contains unexpected content: %s", activityData)
	}
	if types, _ := e.Events(ws.ID, seq); !slices.Contains(types, "activity.created") || !slices.Contains(types, "message.updated") {
		t.Fatalf("edit events %v", types)
	}
}

func TestDeleteInternalNoteRemovesAttachmentsAndPublishesEvents(t *testing.T) {
	e, ws := newEnv(t)
	tk := createTicket(t, e, ws, map[string]any{"channel": "api", "kind": "note"})
	note := createInternalNote(t, e, ws, tk.ID.String())
	key := "workspaces/" + ws.ID.String() + "/attachments/note-delete-test"
	if err := e.D.Storage.Put(e.Ctx, key, strings.NewReader("attachment"), int64(len("attachment")), "text/plain"); err != nil {
		t.Fatal(err)
	}
	e.Exec(`INSERT INTO attachments (workspace_id, message_id, filename, size_bytes, storage_key) VALUES ($1, $2, 'note.txt', 10, $3)`, ws.ID, note.ID, key)
	_, seq := e.Events(ws.ID, 0)

	path := "/v1/tickets/" + tk.ID.String() + "/messages/" + note.ID.String()
	e.Do("DELETE", path, ws.Agent.As(ws), nil).Want(t, 204)
	if n := countRows(t, e, `SELECT count(*) FROM messages WHERE id = $1`, note.ID); n != 0 {
		t.Fatalf("message rows left %d", n)
	}
	if n := countRows(t, e, `SELECT count(*) FROM attachments WHERE message_id = $1`, note.ID); n != 0 {
		t.Fatalf("attachment rows left %d", n)
	}
	if _, err := e.D.Storage.Get(e.Ctx, key); err == nil {
		t.Fatal("attachment object was not removed")
	}
	var activityData string
	if err := e.D.DB.Pool.QueryRow(e.Ctx, `SELECT data::text FROM activities WHERE ticket_id = $1 AND type = 'note_deleted'`, tk.ID).
		Scan(&activityData); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(activityData, note.ID.String()) || strings.Contains(activityData, "Original note") {
		t.Fatalf("activity data contains unexpected content: %s", activityData)
	}
	if types, _ := e.Events(ws.ID, seq); !slices.Contains(types, "activity.created") || !slices.Contains(types, "message.deleted") {
		t.Fatalf("delete events %v", types)
	}
}

func TestInternalNoteMutationPermissionsAndKinds(t *testing.T) {
	e, ws := newEnv(t)
	tk := createTicket(t, e, ws, map[string]any{"channel": "api", "kind": "note"})
	note := createInternalNote(t, e, ws, tk.ID.String())
	path := "/v1/tickets/" + tk.ID.String() + "/messages/" + note.ID.String()
	other := e.AddUser(ws, auth.RoleAgent, "Other Agent")
	e.Do("PATCH", path, other.As(ws), map[string]string{"body_html": "<p>Changed</p>"}).Want(t, 403)
	e.Do("DELETE", path, other.As(ws), nil).Want(t, 403)
	e.Do("DELETE", path, ws.Viewer.As(ws), nil).Want(t, 403)
	e.Do("DELETE", path, ws.Admin.As(ws), nil).Want(t, 204)

	reply := inbound(t, e, ws, tk.ID, "Customer reply")
	replyPath := "/v1/tickets/" + tk.ID.String() + "/messages/" + reply.ID.String()
	e.Do("PATCH", replyPath, ws.Agent.As(ws), map[string]string{"body_html": "<p>Changed</p>"}).Want(t, 422)
	e.Do("DELETE", replyPath, ws.Admin.As(ws), nil).Want(t, 422)
}

func TestDeleteNoteKeepsSharedAttachmentObject(t *testing.T) {
	e, ws := newEnv(t)
	tk := createTicket(t, e, ws, map[string]any{"channel": "api", "kind": "note"})
	note := createInternalNote(t, e, ws, tk.ID.String())
	otherTicket := createTicket(t, e, ws, map[string]any{"channel": "api", "kind": "note"})
	otherNote := createInternalNote(t, e, ws, otherTicket.ID.String())
	key := "workspaces/" + ws.ID.String() + "/attachments/shared-note-test"
	if err := e.D.Storage.Put(e.Ctx, key, strings.NewReader("shared"), int64(len("shared")), "text/plain"); err != nil {
		t.Fatal(err)
	}
	e.Exec(`INSERT INTO attachments (workspace_id, message_id, filename, size_bytes, storage_key) VALUES ($1, $2, 'one.txt', 6, $3)`, ws.ID, note.ID, key)
	e.Exec(`INSERT INTO attachments (workspace_id, message_id, filename, size_bytes, storage_key) VALUES ($1, $2, 'two.txt', 6, $3)`, ws.ID, otherNote.ID, key)
	e.Do("DELETE", "/v1/tickets/"+tk.ID.String()+"/messages/"+note.ID.String(), ws.Agent.As(ws), nil).Want(t, 204)
	if _, err := e.D.Storage.Get(e.Ctx, key); err != nil {
		t.Fatalf("shared attachment object removed: %v", err)
	}
}
