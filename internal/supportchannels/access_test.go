package supportchannels

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/useopendesk/opendesk/internal/auth"
	"github.com/useopendesk/opendesk/internal/helpdesk/hdtest"
	"github.com/useopendesk/opendesk/internal/ids"
	"github.com/useopendesk/opendesk/internal/supportchannels/channel"
)

func TestMayActWith(t *testing.T) {
	agent := &auth.Principal{UserID: uuid.New(), Role: auth.RoleAgent}
	admin := &auth.Principal{UserID: uuid.New(), Role: auth.RoleAdmin}
	owner := &auth.Principal{UserID: uuid.New(), Role: auth.RoleOwner}
	viewer := &auth.Principal{UserID: uuid.New(), Role: auth.RoleViewer}
	for name, tc := range map[string]struct {
		min     string
		allowed []uuid.UUID
		p       *auth.Principal
		want    bool
	}{
		"default lets an agent":        {roleAgent, nil, agent, true},
		"default refuses a viewer":     {roleAgent, nil, viewer, false},
		"admins only lets an admin":    {roleAdmin, nil, admin, true},
		"admins only lets an owner":    {roleAdmin, nil, owner, true},
		"admins only refuses an agent": {roleAdmin, nil, agent, false},
		"a list lets who is on it":     {roleAgent, []uuid.UUID{agent.UserID}, agent, true},
		"a list refuses who is not":    {roleAgent, []uuid.UUID{agent.UserID}, admin, false},
		"a list and a role both hold":  {roleAdmin, []uuid.UUID{agent.UserID}, agent, false},
	} {
		if got := mayActWith(tc.min, tc.allowed, tc.p); got != tc.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestDestinationAccessLimitsWhoActsFromItsThreads(t *testing.T) {
	w := newWorkflow(t)
	status := func() string { s, _ := w.status(t); return s }
	run := func(user, value string) {
		w.fake.posts, w.fake.responses = nil, nil
		w.run(t, channel.Inbound{ExternalUserID: user, Command: "status", Value: value, Text: value})
	}

	// The default: any linked agent.
	run("U1", "pending")
	if status() != "pending" {
		t.Fatalf("an agent could not act: %s", status())
	}

	// Admins only: the agent is told, nothing changes, and the admin still acts.
	w.e.Exec(`UPDATE support_channel_destinations SET min_role = 'admin' WHERE workspace_id = $1`, w.ws.ID)
	run("U1", "solved")
	if status() != "pending" || len(w.fake.posts)+len(w.fake.responses) == 0 || !strings.Contains(strings.Join(append(w.fake.posts, w.fake.responses...), " "), "Ask an admin for access") {
		t.Fatalf("an agent acted or was not told: status=%s posts=%q responses=%q", status(), w.fake.posts, w.fake.responses)
	}
	run("U2", "solved")
	if status() != "solved" {
		t.Fatalf("an admin could not act: %s", status())
	}

	// A list of chosen people: only they, whatever the role.
	w.e.Exec(`UPDATE support_channel_destinations SET min_role = 'agent', allowed_user_ids = $2 WHERE workspace_id = $1`, w.ws.ID, []uuid.UUID{w.ws.Admin.ID})
	run("U1", "open")
	if status() != "solved" {
		t.Fatalf("an agent not on the list acted: %s", status())
	}
	run("U2", "open")
	if status() != "open" {
		t.Fatalf("a person on the list could not act: %s", status())
	}

	// A press of a card's button is acting too, and a provider's edit of a message is dropped without a word.
	w.fake.posts, w.fake.responses = nil, nil
	w.run(t, channel.Inbound{ExternalUserID: "U1", Command: "priority", Value: "urgent", SourceMessageID: "card", Respond: "callback"})
	if _, priority := w.status(t); priority == "urgent" {
		t.Fatal("a card button changed the ticket for someone without access")
	}
}

func TestDestinationAccessAPI(t *testing.T) {
	e := hdtest.New(t, Register)
	ws := e.NewWorkspace()
	connectionID := ids.New()
	credentials, err := e.D.Box.SealJSON(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	e.Exec(`INSERT INTO support_channel_connections(id,workspace_id,provider,name,external_account_id,credentials_enc)
		VALUES($1,$2,'slack','Support','T1',$3)`, connectionID, ws.ID, credentials)
	path := "/v1/support-channels/destinations"
	var d destination
	e.Do(http.MethodPost, path, ws.Admin.As(ws), map[string]any{"connection_id": connectionID.String(), "external_channel_id": "C1"}).Want(t, http.StatusCreated).JSON(t, &d)
	if d.MinRole != roleAgent || d.AllowedUserIDs == nil || len(d.AllowedUserIDs) != 0 {
		t.Fatalf("defaults = %+v", d)
	}
	one := path + "/" + d.ID.String()
	patch := func(body map[string]any, want int) destination {
		t.Helper()
		var out destination
		res := e.Do(http.MethodPatch, one, ws.Admin.As(ws), body).Want(t, want)
		if want == http.StatusOK {
			res.JSON(t, &out)
		}
		return out
	}
	// Admins only, then chosen people (who must be agents or admins of this workspace).
	if got := patch(map[string]any{"min_role": "admin"}, http.StatusOK); got.MinRole != roleAdmin {
		t.Fatalf("min_role = %q", got.MinRole)
	}
	patch(map[string]any{"min_role": "owner"}, http.StatusUnprocessableEntity)
	patch(map[string]any{"allowed_user_ids": []string{"not-an-id"}}, http.StatusUnprocessableEntity)
	patch(map[string]any{"allowed_user_ids": []string{ws.Viewer.ID.String()}}, http.StatusUnprocessableEntity)
	patch(map[string]any{"allowed_user_ids": []string{uuid.NewString()}}, http.StatusUnprocessableEntity)
	other := e.NewWorkspace()
	patch(map[string]any{"allowed_user_ids": []string{other.Admin.ID.String()}}, http.StatusUnprocessableEntity)
	got := patch(map[string]any{"min_role": "agent", "allowed_user_ids": []string{ws.Agent.ID.String(), ws.Agent.ID.String(), ws.Admin.ID.String()}}, http.StatusOK)
	if got.MinRole != roleAgent || len(got.AllowedUserIDs) != 2 {
		t.Fatalf("access = %+v", got)
	}
	// An empty list lets everyone with the role again, and the change is audited.
	if got := patch(map[string]any{"allowed_user_ids": []string{}}, http.StatusOK); len(got.AllowedUserIDs) != 0 {
		t.Fatalf("list = %v", got.AllowedUserIDs)
	}
	var n int
	e.Scalar(`SELECT count(*) FROM audit_logs WHERE workspace_id = $1 AND action = 'support_channel.destination_updated' AND data::text LIKE '%allowed_user_ids%'`, &n, ws.ID)
	if n != 2 {
		t.Fatalf("%d audit entries for access changes, want 2", n)
	}
	// Only an admin person sets it.
	e.Do(http.MethodPatch, one, ws.Agent.As(ws), map[string]any{"min_role": "admin"}).Want(t, http.StatusForbidden)
}
