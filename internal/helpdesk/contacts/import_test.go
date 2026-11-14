package contacts_test

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/useopendesk/opendesk/internal/auth"
	"github.com/useopendesk/opendesk/internal/helpdesk/contacts"
	"github.com/useopendesk/opendesk/internal/helpdesk/hdtest"
	"github.com/useopendesk/opendesk/internal/ids"
)

type contactImportUpload struct {
	UploadID  uuid.UUID `json:"upload_id"`
	Rows      int       `json:"rows"`
	Suggested struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Phone string `json:"phone"`
	} `json:"suggested"`
}

type importState struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"`
	Total     int       `json:"total"`
	Processed int       `json:"processed"`
	Created   int       `json:"created"`
	Updated   int       `json:"updated"`
	Skipped   int       `json:"skipped"`
	Failed    int       `json:"failed"`
	Errors    []struct {
		Line    int    `json:"line"`
		Message string `json:"message"`
	} `json:"errors"`
}

func runImport(t *testing.T, e *hdtest.Env, ws hdtest.WS, a hdtest.Auth, file, onMatch string, mapping map[string]any) importState {
	t.Helper()
	var up contactImportUpload
	e.Upload("/v1/contacts/import/upload", a, "people.csv", []byte(file)).Want(t, 201).JSON(t, &up)
	var x importState
	e.Do("POST", "/v1/contacts/imports", a, map[string]any{"upload_id": up.UploadID, "mapping": mapping, "match_on": "email", "on_match": onMatch}).Want(t, 201).JSON(t, &x)
	if err := contacts.RunImport(e.Ctx, e.D, x.ID); err != nil {
		t.Fatal(err)
	}
	e.Do("GET", "/v1/contacts/imports/"+x.ID.String(), a, nil).Want(t, 200).JSON(t, &x)
	return x
}

func TestContactImport(t *testing.T) {
	e := hdtest.New(t, contacts.Register)
	ws := e.NewWorkspace()
	a := ws.Agent.As(ws)
	e.Exec(`INSERT INTO contacts (id, workspace_id, name, email, phone) VALUES ($1, $2, 'Existing Ann', 'ann@example.com', '')`, ids.New(), ws.ID)

	file := "Full name,E-mail,Phone,Company,Labels\n" +
		"Bob,bob@example.com,555 1,Acme,\"vip; beta\"\n" +
		"Ann Lee,ANN@example.com,555 2,Acme,\n" +
		"No email,not-an-email,,,\n" +
		"Dora,,,,\n"
	mapping := map[string]any{"name": "Full name", "email": "E-mail", "phone": "Phone", "company": "Company", "labels": "Labels"}

	// The headers are matched to fields without being told.
	var up contactImportUpload
	e.Upload("/v1/contacts/import/upload", a, "people.csv", []byte(file)).Want(t, 201).JSON(t, &up)
	if up.Rows != 4 || up.Suggested.Email != "E-mail" || up.Suggested.Name != "Full name" || up.Suggested.Phone != "Phone" {
		t.Fatalf("upload %+v", up)
	}

	// Skip: the existing person is left alone, the others are added, bad rows fail alone.
	x := runImport(t, e, ws, a, file, "skip", mapping)
	if x.Status != "done" || x.Created != 2 || x.Skipped != 1 || x.Failed != 1 || x.Processed != 4 {
		t.Fatalf("skip run %+v", x)
	}
	if len(x.Errors) != 1 || x.Errors[0].Line != 4 {
		t.Fatalf("errors %+v", x.Errors)
	}
	if n := count(e, `SELECT count(*) FROM contacts WHERE workspace_id = $1`, ws.ID); n != 3 {
		t.Fatalf("%d contacts", n)
	}
	var phone string
	e.Scalar(`SELECT phone FROM contacts WHERE workspace_id = $1 AND email = 'ann@example.com'`, &phone, ws.ID)
	if phone != "" {
		t.Fatalf("skip changed Ann: %q", phone)
	}
	if n := count(e, `SELECT count(*) FROM contacts ct JOIN companies co ON co.id = ct.company_id WHERE ct.workspace_id = $1 AND co.name = 'Acme'`, ws.ID); n != 1 {
		t.Fatalf("%d in Acme", n)
	}
	if n := count(e, `SELECT count(*) FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id WHERE t.workspace_id = $1`, ws.ID); n != 2 {
		t.Fatalf("%d labels", n)
	}

	// Fill: Ann has a name already, so only her phone is set. A row without the email it
	// would be found by has nothing to match, so it is added again.
	x = runImport(t, e, ws, a, file, "fill", mapping)
	if x.Created != 1 || x.Updated != 2 || x.Failed != 1 {
		t.Fatalf("fill run %+v", x)
	}
	var name string
	e.Scalar(`SELECT name FROM contacts WHERE workspace_id = $1 AND email = 'ann@example.com'`, &name, ws.ID)
	e.Scalar(`SELECT phone FROM contacts WHERE workspace_id = $1 AND email = 'ann@example.com'`, &phone, ws.ID)
	if name != "Existing Ann" || phone != "555 2" {
		t.Fatalf("fill gave %q %q", name, phone)
	}

	// Overwrite replaces the name.
	x = runImport(t, e, ws, a, file, "overwrite", mapping)
	e.Scalar(`SELECT name FROM contacts WHERE workspace_id = $1 AND email = 'ann@example.com'`, &name, ws.ID)
	if name != "Ann Lee" || x.Updated != 2 || x.Created != 1 {
		t.Fatalf("overwrite gave %q %+v", name, x)
	}
}

func TestContactImportRefusals(t *testing.T) {
	e := hdtest.New(t, contacts.Register)
	ws := e.NewWorkspace()
	a := ws.Agent.As(ws)
	var up contactImportUpload
	e.Upload("/v1/contacts/import/upload", a, "people.csv", []byte("Name,Email\nA,a@example.com\n")).Want(t, 201).JSON(t, &up)
	start := func(mapping map[string]any, matchOn string) *hdtest.Resp {
		return e.Do("POST", "/v1/contacts/imports", a, map[string]any{"upload_id": up.UploadID, "mapping": mapping, "match_on": matchOn, "on_match": "skip"})
	}
	start(map[string]any{"email": "Nope"}, "email").Want(t, 422)
	start(map[string]any{}, "email").Want(t, 422)
	start(map[string]any{"name": "Name"}, "email").Want(t, 422)
	start(map[string]any{"email": "Email"}, "external_id").Want(t, 422)
	e.Upload("/v1/contacts/import/upload", a, "people.exe", []byte("Name,Email\nA,a@b.co\n")).Want(t, 422)
	e.Upload("/v1/contacts/import/upload", a, "people.csv", []byte("Name,Email\n")).Want(t, 422)

	// Another workspace's upload is not found.
	other := e.NewWorkspace()
	e.Do("POST", "/v1/contacts/imports", other.Agent.As(other), map[string]any{"upload_id": up.UploadID, "mapping": map[string]any{"email": "Email"}, "match_on": "email", "on_match": "skip"}).Want(t, 404)
	// A viewer cannot import.
	v := e.AddUser(ws, auth.RoleViewer, "Viewer").As(ws)
	e.Upload("/v1/contacts/import/upload", v, "people.csv", []byte("Name,Email\nA,a@example.com\n")).Want(t, 403)
	e.Do("POST", "/v1/contacts/export", v, map[string]any{"query": map[string]any{}}).Want(t, 403)
}

func TestContactExport(t *testing.T) {
	e := hdtest.New(t, contacts.Register)
	ws := e.NewWorkspace()
	a := ws.Agent.As(ws)
	e.Exec(`INSERT INTO contacts (id, workspace_id, name, email, notes) VALUES ($1, $2, '=HYPERLINK("x")', 'a@example.com', 'hello, "world"')`, ids.New(), ws.ID)
	e.Exec(`INSERT INTO contacts (id, workspace_id, name, email) VALUES ($1, $2, 'Bee', 'b@example.com')`, ids.New(), ws.ID)
	e.Exec(`INSERT INTO contacts (id, workspace_id, name, email) VALUES ($1, $2, 'Other workspace', 'c@example.com')`, ids.New(), e.NewWorkspace().ID)

	r := e.Do("POST", "/v1/contacts/export", a, map[string]any{"query": map[string]any{}}).Want(t, 200)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content type %q", ct)
	}
	if cd := r.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Fatalf("disposition %q", cd)
	}
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(r.Body), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || recs[0][0] != "Name" || recs[0][1] != "Email" {
		t.Fatalf("export %v", recs)
	}
	for _, rec := range recs[1:] {
		if strings.HasPrefix(rec[0], "=") {
			t.Fatalf("a formula went out as is: %q", rec[0])
		}
		if rec[1] == "c@example.com" {
			t.Fatal("another workspace's person was exported")
		}
	}

	// By id: one person; an id of another workspace is not found.
	var id string
	e.Scalar(`SELECT id::text FROM contacts WHERE workspace_id = $1 AND email = 'b@example.com'`, &id, ws.ID)
	r = e.Do("POST", "/v1/contacts/export", a, map[string]any{"ids": []string{id}}).Want(t, 200)
	if n := strings.Count(string(r.Body), "\n"); n != 2 {
		t.Fatalf("%d lines:\n%s", n, r.Body)
	}
	var foreign string
	e.Scalar(`SELECT id::text FROM contacts WHERE email = 'c@example.com' AND workspace_id <> $1 LIMIT 1`, &foreign, ws.ID)
	e.Do("POST", "/v1/contacts/export", a, map[string]any{"ids": []string{foreign}}).Want(t, 404)
	e.Do("POST", "/v1/contacts/export", a, map[string]any{}).Want(t, 422)
}

func TestContactImportStarter(t *testing.T) {
	e := hdtest.New(t, contacts.Register)
	ws := e.NewWorkspace()
	file := "Name,Email\nKai,kai@example.com\n"
	mapping := map[string]any{"name": "Name", "email": "Email"}

	// Started with an API key: no one to name in the audit log, and the import still ends.
	key := hdtest.Auth{Bearer: e.APIKey(ws, auth.ScopeContactsRead, auth.ScopeContactsWrite)}
	x := runImport(t, e, ws, key, file, "skip", mapping)
	if x.Status != "done" || x.Created != 1 {
		t.Fatalf("api key run %+v", x)
	}
	if n := count(e, `SELECT count(*) FROM audit_logs WHERE workspace_id = $1 AND entity_id = $2 AND action = 'contact.import_done' AND actor_user_id IS NULL`, ws.ID, x.ID); n != 1 {
		t.Fatalf("%d audit rows", n)
	}

	// Started by someone whose account is deleted before it ran: it ends all the same.
	gone := e.AddUser(ws, auth.RoleAgent, "Gone Agent")
	var up contactImportUpload
	e.Upload("/v1/contacts/import/upload", gone.As(ws), "people.csv", []byte("Name,Email\nLeo,leo@example.com\n")).Want(t, 201).JSON(t, &up)
	e.Do("POST", "/v1/contacts/imports", gone.As(ws), map[string]any{"upload_id": up.UploadID, "mapping": mapping, "match_on": "email", "on_match": "skip"}).Want(t, 201).JSON(t, &x)
	e.Exec(`DELETE FROM users WHERE id = $1`, gone.ID)
	if err := contacts.RunImport(e.Ctx, e.D, x.ID); err != nil {
		t.Fatal(err)
	}
	var status string
	e.Scalar(`SELECT status FROM contact_imports WHERE id = $1`, &status, x.ID)
	if status != "done" {
		t.Fatalf("deleted starter: %s", status)
	}

	// Started by someone who has since left the workspace: it stops without adding anyone.
	left := e.AddUser(ws, auth.RoleAgent, "Left Agent")
	e.Upload("/v1/contacts/import/upload", left.As(ws), "people.csv", []byte("Name,Email\nMo,mo@example.com\n")).Want(t, 201).JSON(t, &up)
	e.Do("POST", "/v1/contacts/imports", left.As(ws), map[string]any{"upload_id": up.UploadID, "mapping": mapping, "match_on": "email", "on_match": "skip"}).Want(t, 201).JSON(t, &x)
	e.Exec(`DELETE FROM members WHERE workspace_id = $1 AND user_id = $2`, ws.ID, left.ID)
	if err := contacts.RunImport(e.Ctx, e.D, x.ID); err != nil {
		t.Fatal(err)
	}
	e.Scalar(`SELECT status FROM contact_imports WHERE id = $1`, &status, x.ID)
	if status != "failed" || count(e, `SELECT count(*) FROM contacts WHERE workspace_id = $1 AND email = 'mo@example.com'`, ws.ID) != 0 {
		t.Fatalf("left member: %s", status)
	}
}

func TestContactImportMatchesExtraAddress(t *testing.T) {
	e := hdtest.New(t, contacts.Register)
	ws := e.NewWorkspace()
	a := ws.Agent.As(ws)
	id := ids.New()
	e.Exec(`INSERT INTO contacts (id, workspace_id, name, email, phone) VALUES ($1, $2, 'Ann', 'ann@example.com', '')`, id, ws.ID)
	e.Exec(`INSERT INTO contact_emails (workspace_id, contact_id, email) VALUES ($1, $2, 'ann.work@example.com')`, ws.ID, id)

	// Found by an extra address and overwritten: the primary address stays.
	x := runImport(t, e, ws, a, "Name,Email\nAnn Lee,ANN.WORK@example.com\n", "overwrite", map[string]any{"name": "Name", "email": "Email"})
	if x.Status != "done" || x.Updated != 1 || x.Failed != 0 {
		t.Fatalf("run %+v", x)
	}
	var name, email string
	e.Scalar(`SELECT name FROM contacts WHERE id = $1`, &name, id)
	e.Scalar(`SELECT email::text FROM contacts WHERE id = $1`, &email, id)
	if name != "Ann Lee" || email != "ann@example.com" {
		t.Fatalf("got %q %q", name, email)
	}
}

func TestContactImportCompanyNames(t *testing.T) {
	e := hdtest.New(t, contacts.Register)
	ws := e.NewWorkspace()
	a := ws.Agent.As(ws)
	file := "Name,Email,Company\nA,,\"  Acme    Corp \"\nB,,acme corp\nC,,<b>Bad</b>\n"
	x := runImport(t, e, ws, a, file, "skip", map[string]any{"name": "Name", "email": "Email", "company": "Company"})
	if x.Created != 2 || x.Failed != 1 || len(x.Errors) != 1 || x.Errors[0].Line != 4 {
		t.Fatalf("run %+v", x)
	}
	if n := count(e, `SELECT count(*) FROM companies WHERE workspace_id = $1`, ws.ID); n != 1 {
		t.Fatalf("%d companies", n)
	}
	var name string
	e.Scalar(`SELECT name FROM companies WHERE workspace_id = $1`, &name, ws.ID)
	if name != "Acme Corp" {
		t.Fatalf("company %q", name)
	}
}

func TestContactExportImportRoundTrip(t *testing.T) {
	e := hdtest.New(t, contacts.Register)
	ws := e.NewWorkspace()
	a := ws.Agent.As(ws)
	e.Exec(`INSERT INTO custom_fields (workspace_id, entity, key, name, type) VALUES ($1, 'contact', 'balance', 'Balance', 'number')`, ws.ID)
	e.Exec(`INSERT INTO contacts (id, workspace_id, name, email, phone, fields) VALUES ($1, $2, 'Ann', 'ann@example.com', '+1 555 0100', '{"balance": -5}')`, ids.New(), ws.ID)

	r := e.Do("POST", "/v1/contacts/export", a, map[string]any{"query": map[string]any{}}).Want(t, 200)
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(r.Body), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	header := recs[0]
	col := func(want string) string {
		for _, h := range header {
			if h == want {
				return h
			}
		}
		t.Fatalf("no %q column in %v", want, header)
		return ""
	}
	e.Exec(`DELETE FROM contacts WHERE workspace_id = $1`, ws.ID)

	x := runImport(t, e, ws, a, string(r.Body), "skip", map[string]any{"name": col("Name"), "email": col("Email"), "phone": col("Phone"),
		"fields": map[string]any{"balance": col("Balance")}})
	if x.Status != "done" || x.Created != 1 || x.Failed != 0 {
		t.Fatalf("run %+v", x)
	}
	var phone, balance string
	e.Scalar(`SELECT phone FROM contacts WHERE workspace_id = $1`, &phone, ws.ID)
	e.Scalar(`SELECT fields->>'balance' FROM contacts WHERE workspace_id = $1`, &balance, ws.ID)
	if phone != "+1 555 0100" || balance != "-5" {
		t.Fatalf("round trip gave %q %q", phone, balance)
	}
}
