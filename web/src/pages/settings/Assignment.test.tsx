import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/i18n";
import { ConfirmProvider } from "@/components/ui/confirm";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useSession } from "@/lib/session";
import { installMock } from "@/mock";
import { db } from "@/mock/db";
import { dispatch } from "@/mock/router";
import Assignment from "./Assignment";

vi.mock("@tanstack/react-router", async (load) => {
  const actual = await load<typeof import("@tanstack/react-router")>();
  return { ...actual, Link: ({ to, children, ...props }: React.ComponentProps<"a"> & { to: string }) => <a href={to} {...props}>{children}</a> };
});

vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value() {} });
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const settle = () => act(async () => new Promise((resolve) => setTimeout(resolve, 30)));

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
  let root: Root;
  let el: HTMLDivElement;

  beforeEach(() => {
    saved = ws.skills;
    el = document.createElement("div");
    document.body.appendChild(el);
    root = createRoot(el);
    useSession.setState({ status: "authed", workspaceId: ws.workspace.id, memberships: [{ workspace: ws.workspace, role: "admin" }] } as never);
  });
  afterEach(() => {
    ws.skills = saved;
    act(() => root.unmount());
    el.remove();
  });

  const mount = async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    await act(async () => root.render(<QueryClientProvider client={qc}><TooltipProvider><ConfirmProvider><Assignment /></ConfirmProvider></TooltipProvider></QueryClientProvider>));
    for (let i = 0; i < 100 && !el.textContent?.includes("Billing"); i++) await settle();
  };

  it("renders the catalog and creates a skill from Assignment settings", async () => {
    await mount();
    expect(el.textContent).toContain("Routing skills");
    expect(el.textContent).toContain("Billing");
    const add = [...el.querySelectorAll("button")].find((button) => button.textContent?.includes("Add skill"));
    expect(add).toBeDefined();
    await act(async () => add!.click());
    await settle();
    const input = document.querySelector<HTMLInputElement>('input[aria-label="Skill name"]');
    expect(input).toBeTruthy();
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
    await act(async () => {
      setter.call(input, "UI-created skill");
      input!.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await settle();
    const create = [...document.querySelectorAll("[cmdk-item]")].find((item) => item.textContent?.includes("UI-created skill"));
    expect(create).toBeDefined();
    await act(async () => (create as HTMLElement).click());
    for (let i = 0; i < 20 && !ws.skills.some((skill) => skill.name === "UI-created skill"); i++) await settle();
    expect(ws.skills.some((skill) => skill.name === "UI-created skill")).toBe(true);
  });

  it("creates a skill and assigns it to eligible teammates through the mock API", async () => {
    ws.skills = [];
    const created = await call("POST", "/v1/skills", { name: "  Billing   support " });
    expect(created.status).toBe(201);
    expect(created.body.name).toBe("Billing support");
    const memberIds = ws.members.filter((member) => member.role !== "viewer").slice(0, 2).map((member) => member.user_id);
    const assigned = await call("PUT", `/v1/skills/${created.body.id}/members`, { member_ids: memberIds });
    expect(assigned.status).toBe(200);
    expect(assigned.body.member_ids).toEqual(memberIds);
  });
});
