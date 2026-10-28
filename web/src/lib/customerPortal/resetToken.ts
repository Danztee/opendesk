/** Capture the one-time token before rendering the route and remove it from browser history. */
export function captureResetToken(): string {
  const url = new URL(window.location.href);
  const token = url.searchParams.get("token") ?? "";
  url.searchParams.delete("token");
  window.history.replaceState(window.history.state, "", url.pathname + url.search + url.hash);
  return token;
}
