"use strict";
// Separate account-level readings: never mix Codex buckets into Claude history.
(() => {
  const box = $("codex-limits");
  let result = null, loading = false;
  let lastAttempt = 0;
  function label(w, index) {
    const n = w?.windowDurationMins;
    if (n === 300) return "5-hour limit";
    if (n === 10080) return "Weekly limit";
    return n > 0 ? dur(n * MIN) + " window" : index ? "Secondary limit" : "Primary limit";
  }
  function windowTile(w, index) {
    const expired = w?.resetsAt != null && w.resetsAt * 1000 <= now();
    const used = w?.usedPercent;
    const known = Number.isFinite(used) && used >= 0 && !expired;
    const bounded = known ? Math.min(100, used) : 0;
    const reset = w?.resetsAt ? new Date(w.resetsAt * 1000).toLocaleString() : "not reported";
    return h("div", { class: "tile" }, h("div", { class: "lab" }, label(w, index)),
      h("div", { class: "num" }, known ? Math.round(used) + "%" : "—"),
      h("div", { class: "sub2" }, known ? "used · " + Math.round(Math.max(0, 100 - used)) + "% remaining" : expired ? "Window ended; awaiting an updated reading" : "Not reported by Codex"),
      h("div", { class: "pace-track", role: "meter", "aria-label": label(w, index) + " used", "aria-valuemin": "0", "aria-valuemax": "100", "aria-valuenow": known ? bounded : null },
        h("div", { class: "pace-fill c-" + tone3(bounded), style: "width:" + bounded + "%" })),
      h("div", { class: "sub2" }, "Resets: " + reset));
  }
  function draw() {
    if (!result) return;
    const stale = result.stale || (result.updated_at && now() - result.updated_at > 120e3);
    const cards = (result.buckets || []).map(b => h("section", { class: "panel chart-panel" },
      h("div", { class: "chart-head" }, h("h2", null, "Codex · " + (b.limitName || b.limitId))),
      h("div", { class: "tiles codex-tiles" }, windowTile(b.primary, 0), windowTile(b.secondary, 1))));
    box.replaceChildren(...[
      h("p", { class: "meta" }, "Account usage shared across sessions using this Codex CLI sign-in. " + (result.updated_at ? (stale ? "Stale reading · " : "Updated ") + new Date(result.updated_at).toLocaleTimeString() : "")),
      result.error ? h("p", { class: "c-yellow", role: "status" }, result.error) : null,
      ...cards,
      !cards.length ? h("section", { class: "panel chart-panel" }, h("h2", null, "Codex usage unavailable"),
        h("p", null, "No quota windows were returned. Check that the Codex CLI is installed and signed in to your ChatGPT account.")) : null,
      h("p", { class: "meta" }, "Refreshes once a minute; retries failed reads after 15 seconds. These are current quota readings; historical pace projections are not available.")].filter(Boolean));
  }
  async function loadCodex() {
    if ((document.hidden && result) || loading) return;
    if (Date.now() - lastAttempt < (result?.stale ? 15e3 : 60e3)) return;
    loading = true;
    try {
      const response = await fetch("/v1/codex/limits");
      if (!response.ok) throw new Error("unavailable");
      result = await response.json();
    } catch {
      result = { ...(result || {}), stale: true, error: "Could not reach TRK's Codex usage endpoint. Check that TRK is running." };
    } finally { lastAttempt = Date.now(); loading = false; draw(); }
  }
  document.addEventListener("visibilitychange", () => { if (!document.hidden) loadCodex(); });
  addEventListener("pageshow", () => loadCodex());
  setInterval(loadCodex, 15e3);
  loadCodex();
})();
