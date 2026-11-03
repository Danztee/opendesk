package tickets_test

import (
	"testing"

	"github.com/useopendesk/opendesk/internal/helpdesk/hdtest"
)

// With Tickets off every route tagged Tickets answers feature_disabled, for members and API
// keys alike, and answers again once it is back on.
func TestTicketsOffRefusesTicketRoutes(t *testing.T) {
	e, ws := newEnv(t)
	tk := createTicket(t, e, ws, map[string]any{"kind": "note"})
	key := e.APIKey(ws, "tickets:read", "tickets:write")
	e.SetSettings(ws, map[string]any{"tickets_enabled": false})

	for _, a := range []struct {
		name string
		auth hdtest.Auth
	}{{"member", ws.Admin.As(ws)}, {"api key", hdtest.Auth{Bearer: key}}} {
		for _, r := range [][2]string{{"GET", "/v1/tickets"}, {"GET", "/v1/tickets/counts"}, {"GET", "/v1/tickets/" + tk.ID.String()},
			{"GET", "/v1/tickets/" + tk.ID.String() + "/messages"}} {
			var out struct {
				Code, Product, Reason string
			}
			e.Do(r[0], r[1], a.auth, nil).Want(t, 403).JSON(t, &out)
			if out.Code != "feature_disabled" || out.Product != "tickets" || out.Reason != "workspace_disabled" {
				t.Fatalf("%s %s %s: %+v", a.name, r[0], r[1], out)
			}
		}
		e.Do("POST", "/v1/tickets", a.auth, map[string]any{"subject": "x", "body_html": "<p>x</p>", "kind": "note"}).Want(t, 403)
	}

	e.SetSettings(ws, map[string]any{"tickets_enabled": true})
	e.Do("GET", "/v1/tickets/"+tk.ID.String(), ws.Admin.As(ws), nil).Want(t, 200)
}
