import { afterEach, describe, expect, it } from "vitest";
import { installMock } from "@/mock";
import { db } from "@/mock/db";
import { dispatch } from "@/mock/router";

async function call(method: string, path: string, body?: unknown) {
  const ws = db.workspaces[0].workspace.id;
  const response = await dispatch(`http://mock${path}`, {
    method,
    body: body === undefined ? undefined : JSON.stringify(body),
    headers: { "OD-Workspace": ws },
  });
  return { status: response.status, body: response.status === 204 ? null : await response.json() };
}

describe("assignment skill catalog", () => {
  installMock();
  const ws = db.workspaces[0];
  let saved = ws.skills;
  afterEach(() => { ws.skills = saved; });

  it("creates a skill and assigns it to eligible teammates", async () => {
    saved = ws.skills;
    ws.skills = [];
    const created = await call("POST", "/v1/skills", { name: "  Billing   support " });
    expect(created.status).toBe(201);
    expect(created.body.name).toBe("Billing support");

    const memberIds = ws.members.filter((member) => member.role !== "viewer").slice(0, 2).map((member) => member.user_id);
    const assigned = await call("PUT", `/v1/skills/${created.body.id}/members`, { member_ids: memberIds });
    expect(assigned.status).toBe(200);
    expect(assigned.body.member_ids).toEqual(memberIds);
    expect((await call("GET", "/v1/skills")).body.data).toHaveLength(1);
  });

  it("prevents archiving a skill used by an enabled rule", async () => {
    saved = ws.skills;
    const skill = ws.skills[0];
    const rule = ws.routingRules.find((item) => item.enabled);
    expect(rule).toBeDefined();
    const original = rule!.skill_ids;
    rule!.skill_ids = [...original, skill.id];
    const result = await call("PATCH", `/v1/skills/${skill.id}`, { archived: true });
    rule!.skill_ids = original;
    expect(result.status).toBe(409);
  });
});
