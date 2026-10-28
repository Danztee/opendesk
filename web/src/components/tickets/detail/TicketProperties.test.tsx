import { describe, expect, it } from "vitest";
import { ticket } from "@/lib/test/fixtures";

describe("ticket routing skill labels", () => {
  it("keeps required skill labels on both assigned and unassigned tickets", () => {
    const required_skills = [{ id: "skill-billing", name: "Billing" }];
    expect(ticket({ required_skills, assignee: { id: "agent-1", name: "Ada", email: "ada@example.com", avatar_url: null } }).required_skills).toEqual(required_skills);
    expect(ticket({ required_skills, assignee: null }).required_skills).toEqual(required_skills);
  });
});
