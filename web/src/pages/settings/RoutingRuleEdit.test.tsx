import { afterEach, describe, expect, it } from "vitest";
import { installMock } from "@/mock";
import { db } from "@/mock/db";
import { evaluateRouting } from "@/mock/teams";

describe("skill-based routing", () => {
  installMock();
  const ws = db.workspaces[0];
  let saved = ws.routingRules;
  afterEach(() => { ws.routingRules = saved; });

  it("returns the matched rule's required skills in the routing result", () => {
    saved = ws.routingRules;
    const skill = ws.skills[0];
    const rule = {
      id: "skill-rule-test", name: "Billing questions", enabled: true, position: -1, run_count: 0,
      match: "all" as const, conditions: [{ field: "subject" as const, op: "contains" as const, value: "skill test" }],
      team_id: ws.teams[0].id, skill_ids: [skill.id], created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    };
    ws.routingRules = [rule, ...saved];
    const result = evaluateRouting(ws, { channel: "email", to: ws.mailboxes[0].email, subject: "Skill test", body: "" });
    expect(result.routed?.rule_id).toBe(rule.id);
    expect(result.required_skills.map((item) => item.id)).toEqual([skill.id]);
  });
});
