import { QueryClientProvider } from "@tanstack/react-query";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { AuthForm, Ticket, TicketList } from "./CustomerPortal";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { isPublicPath } from "@/i18n";
import { getAccessToken, setAccessToken } from "@/lib/api/client";
import { captureResetToken } from "@/lib/customerPortal/resetToken";
import { hasSecretParams } from "@/lib/analytics";
import { clearCustomer, customerPortal, refreshCustomer } from "@/lib/customerPortal/client";
import { queryClient } from "@/lib/queryClient";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, ...props }: { children: ReactNode; to: string; params: unknown }) => <a href="#" {...{ className: (props as { className?: string }).className }}>{children}</a>,
}));

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
afterEach(() => { clearCustomer(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("customer portal", () => {
  it("classifies portal routes as public without matching similar paths", () => {
    expect(isPublicPath("/portal/acme/reset")).toBe(true);
    expect(isPublicPath("/portal/acme/tickets/123")).toBe(true);
    expect(isPublicPath("/portals")).toBe(false);
  });
  it("does not use a weak-password response and clears customer cache without touching agent data", async () => {
    setAccessToken("agent-token");
    queryClient.setQueryData(["customerPortal", "acme", "tickets"], ["private"]);
    queryClient.setQueryData(["agent", "tickets"], ["agent"]);
    const fetch = vi.fn().mockResolvedValueOnce(json({ change_password: true }));
    vi.stubGlobal("fetch", fetch);
    expect((await customerPortal.login("acme", "a@example.com", "password")).change_password).toBe(true);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][1]).toMatchObject({ credentials: "include" });
    expect(queryClient.getQueryData(["customerPortal", "acme", "tickets"])).toBeUndefined();
    expect(queryClient.getQueryData(["agent", "tickets"])).toEqual(["agent"]);
    expect(getAccessToken()).toBe("agent-token");
    setAccessToken(null);
  });
  it("clears customer data on logout and sends the customer bearer", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(json({ access_token: "customer", expires_at: "later" })).mockResolvedValueOnce(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetch);
    await customerPortal.login("acme", "a@example.com", "password");
    queryClient.setQueryData(["customerPortal", "acme", "tickets"], ["private"]);
    await customerPortal.logout();
    expect(fetch.mock.calls[1][1]).toMatchObject({ credentials: "include", headers: { Authorization: "Bearer customer" } });
    expect(queryClient.getQueryData(["customerPortal", "acme", "tickets"])).toBeUndefined();
  });
  it("serializes refreshes and checks the slug for each workspace", async () => {
    let resolve!: (response: Response) => void;
    const fetch = vi.fn().mockImplementationOnce(() => new Promise<Response>((r) => { resolve = r; })).mockResolvedValueOnce(json({ access_token: "second" }));
    vi.stubGlobal("fetch", fetch);
    const first = refreshCustomer("first");
    const same = refreshCustomer("first");
    const second = refreshCustomer("second");
    expect(fetch).toHaveBeenCalledTimes(1);
    resolve(json({ access_token: "first" }));
    expect(await first).toBe(true);
    expect(await same).toBe(true);
    expect(await second).toBe(true);
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({ workspace_slug: "second" });
    expect(fetch.mock.calls[1][1]).toMatchObject({ credentials: "include", referrerPolicy: "no-referrer" });
  });
});

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

it("shows an invalid-link message when a reset token is missing", async () => {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  await act(async () => root.render(<QueryClientProvider client={queryClient}><AuthForm slug="acme" mode="reset" resetToken="" onLogin={() => {}} /></QueryClientProvider>));
  expect(container.textContent).toContain("This link has expired or is invalid");
  expect(container.querySelector('input[type="password"]')).toBeNull();
  expect(container.querySelector('a[href="/portal/acme/forgot"]')).not.toBeNull();
  await act(async () => root.unmount());
  container.remove();
});

it("captures and removes the reset token before subsequent requests or analytics", () => {
  history.replaceState(null, "", "/portal/acme/reset?lang=fr&token=secret#section");
  expect(hasSecretParams(location.search)).toBe(true);
  expect(captureResetToken()).toBe("secret");
  expect(location.href).not.toContain("secret");
  expect(location.search).toBe("?lang=fr");
  expect(hasSecretParams(location.search)).toBe(false);
  history.replaceState(null, "", "/");
});

it("offers a reset link instead of opening tickets after a weak-password login", async () => {
  const fetch = vi.fn().mockResolvedValue(json({ change_password: true }));
  vi.stubGlobal("fetch", fetch);
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const onLogin = vi.fn();
  await act(async () => root.render(<QueryClientProvider client={queryClient}><AuthForm slug="acme" mode="login" resetToken="" onLogin={onLogin} /></QueryClientProvider>));
  const inputs = container.querySelectorAll("input");
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  await act(async () => {
    setValue.call(inputs[0], "customer@example.com"); inputs[0].dispatchEvent(new Event("input", { bubbles: true }));
    setValue.call(inputs[1], "old-password"); inputs[1].dispatchEvent(new Event("input", { bubbles: true }));
  });
  await act(async () => { container.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); });
  expect(onLogin).not.toHaveBeenCalled();
  expect(container.textContent).toContain("Choose a new password before signing in");
  expect(container.querySelector('a[href="/portal/acme/forgot"]')).not.toBeNull();
  await act(async () => root.unmount());
  container.remove();
});


it("shows list loading, empty, error and ticket rows", async () => {
  const container = document.createElement("div"); document.body.append(container);
  const root = createRoot(container);
  let resolve!: (value: { data: []; next_cursor: null }) => void;
  const list = vi.spyOn(customerPortal, "tickets").mockImplementationOnce(() => new Promise((r) => { resolve = r; }));
  await act(async () => root.render(<QueryClientProvider client={queryClient}><TicketList slug="list-test" helpURL="https://help.example" /></QueryClientProvider>));
  expect(container.textContent).toContain("Loading");
  await act(async () => { resolve({ data: [], next_cursor: null }); await Promise.resolve(); });
  await vi.waitFor(() => expect(container.textContent).toContain("You have no requests"));
  expect(container.querySelector('a[href="https://help.example"]')).not.toBeNull();
  await act(async () => root.unmount()); clearCustomer();
  list.mockRejectedValueOnce(new Error("unavailable"));
  const next = createRoot(container);
  await act(async () => next.render(<QueryClientProvider client={queryClient}><TicketList slug="list-error" helpURL="https://help.example" /></QueryClientProvider>));
  await vi.waitFor(() => expect(container.textContent).toContain("Something went wrong"));
  await act(async () => next.unmount()); clearCustomer();
  list.mockResolvedValueOnce({ data: [{ id: "1", number: 42, subject: "Delivery", status: "open", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }], next_cursor: null });
  const rows = createRoot(container);
  await act(async () => rows.render(<QueryClientProvider client={queryClient}><TicketList slug="list-rows" helpURL="https://help.example" /></QueryClientProvider>));
  await vi.waitFor(() => expect(container.textContent).toContain("Delivery"));
  expect(container.textContent).toContain("Open");
  expect(container.querySelector("a")?.tabIndex).toBe(0);
  await act(async () => rows.unmount()); container.remove();
});

it("loads a transcript and submits a reply with the keyboard", async () => {
  vi.spyOn(customerPortal, "ticket").mockResolvedValue({ id: "1", number: 42, subject: "Delivery", status: "open", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" });
  vi.spyOn(customerPortal, "messages").mockResolvedValue({ data: [{ id: "m", direction: "outbound", body_text: "We are checking.", created_at: "2026-01-01T00:00:00Z" }], next_cursor: null });
  const reply = vi.spyOn(customerPortal, "reply").mockResolvedValue({ id: "n", direction: "inbound", body_text: "Thanks", created_at: "2026-01-01T00:00:00Z" });
  const container = document.createElement("div"); document.body.append(container); const root = createRoot(container);
  await act(async () => root.render(<QueryClientProvider client={queryClient}><Ticket slug="detail" id="1" /></QueryClientProvider>));
  await vi.waitFor(() => expect(container.textContent).toContain("We are checking."));
  const input = container.querySelector("textarea")!;
  await act(async () => { input.focus(); Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(input, "Thanks"); input.dispatchEvent(new Event("input", { bubbles: true })); });
  expect(document.activeElement).toBe(input);
  const button = container.querySelector('button[type="submit"]')! as HTMLButtonElement;
  await act(async () => { button.focus(); button.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })); button.click(); });
  await vi.waitFor(() => expect(reply).toHaveBeenCalledWith("detail", "1", "Thanks"));
  await act(async () => root.unmount()); container.remove();
});

it("shows detail loading, empty conversation and errors", async () => {
  const ticket = { id: "1", number: 42, subject: "Delivery", status: "open", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" };
  let resolve!: (value: typeof ticket) => void;
  const getTicket = vi.spyOn(customerPortal, "ticket").mockImplementationOnce(() => new Promise((r) => { resolve = r; }));
  vi.spyOn(customerPortal, "messages").mockResolvedValue({ data: [], next_cursor: null });
  const container = document.createElement("div"); document.body.append(container); const root = createRoot(container);
  await act(async () => root.render(<QueryClientProvider client={queryClient}><Ticket slug="detail-empty" id="1" /></QueryClientProvider>));
  expect(container.textContent).toContain("Loading");
  await act(async () => { resolve(ticket); await Promise.resolve(); });
  await vi.waitFor(() => expect(container.textContent).toContain("No messages yet"));
  await act(async () => root.unmount()); clearCustomer();
  getTicket.mockRejectedValueOnce(new Error("missing"));
  const error = createRoot(container);
  await act(async () => error.render(<QueryClientProvider client={queryClient}><Ticket slug="detail-error" id="1" /></QueryClientProvider>));
  await vi.waitFor(() => expect(container.textContent).toContain("Something went wrong"));
  expect(container.querySelector("textarea")).toBeNull();
  await act(async () => error.unmount()); container.remove();
});
