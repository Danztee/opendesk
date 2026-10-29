package routing

import (
	"testing"
	"time"

	"github.com/useopendesk/opendesk/internal/helpdesk/hdtest"
)

func TestSkillMembersWaitForAccountBeforeWorkspace(t *testing.T) {
	w := newWorld(t)
	e := w.e
	var skill skillResponse
	e.Do("POST", "/v1/skills", w.admin(), map[string]any{"name": "French"}).Want(t, 201).JSON(t, &skill)
	gate, err := e.D.DB.Pool.Begin(e.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Rollback(e.Ctx)
	var pid int32
	// Account/profile writers lock the account before publishing member events.
	if err := gate.QueryRow(e.Ctx, `SELECT pg_backend_pid() FROM users WHERE id=$1 FOR UPDATE`, w.ws.Agent.ID).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan *hdtest.Resp, 1)
	go func() {
		done <- e.Do("PUT", "/v1/skills/"+skill.ID.String()+"/members", w.admin(), map[string]any{"member_ids": []string{w.ws.Agent.ID.String()}})
	}()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond * 10) {
		var blocked bool
		if err := e.D.DB.Pool.QueryRow(e.Ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("skill update never waited for account")
		}
	}
	_, lockErr := gate.Exec(e.Ctx, `SELECT 1 FROM workspaces WHERE id=$1 FOR UPDATE NOWAIT`, w.ws.ID)
	gate.Rollback(e.Ctx)
	(<-done).Want(t, 200)
	if lockErr != nil {
		t.Fatalf("account writer could not publish its workspace event: %v", lockErr)
	}
	var n int
	if err := e.D.DB.Pool.QueryRow(e.Ctx, `SELECT count(*) FROM member_skills WHERE workspace_id=$1 AND skill_id=$2 AND user_id=$3`, w.ws.ID, skill.ID, w.ws.Agent.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("eligible member was not retained: %d", n)
	}
}
