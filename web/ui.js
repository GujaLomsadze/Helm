"use strict";
// UI inventory (ui.html): renders TRK's components from fixtures with the real renderers in
// app.js, and lists every colour token with its contrast. Open /ui.html on a dev daemon after
// a visual change; nothing links here from the dashboard.

kit.set([
  { name: "claude", label: "Claude Code", seen: true, available: true, limits: false, counters: true, resume: "claude --resume {id}", icon: true },
  { name: "codex", label: "Codex", seen: true, available: true, limits: true, counters: false, resume: "codex resume {id}", icon: true },
]);
multi = true;
skew = 0;

// ---- tokens ----
const probe = document.createElement("canvas").getContext("2d");
function rgbOf(css) {
  probe.fillStyle = "#000";
  probe.fillStyle = css;
  const v = probe.fillStyle; // normalized: #rrggbb or rgba(r, g, b, a)
  if (v.startsWith("#")) return [1, 3, 5].map((i) => parseInt(v.slice(i, i + 2), 16));
  const m = v.match(/[\d.]+/g);
  return m ? m.slice(0, 3).map(Number) : null;
}
function luminance([r, g, b]) {
  const f = (c) => { c /= 255; return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4; };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}
const contrast = (a, b) => { const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p); return (x + 0.05) / (y + 0.05); };

async function tokens() {
  const css = await (await fetch("theme.css")).text();
  const root = (css.match(/:root\s*\{([^}]*)\}/) || [])[1] || "";
  const cs = getComputedStyle(document.documentElement);
  const bg = rgbOf(cs.getPropertyValue("--bg").trim()), panel = rgbOf(cs.getPropertyValue("--panel").trim());
  const rows = [];
  for (const [, name] of root.matchAll(/(--[\w-]+)\s*:/g)) {
    const value = cs.getPropertyValue(name).trim();
    if (/^(--font|--radius|--glow)/.test(name)) continue;
    const rgb = rgbOf(value);
    if (!rgb) continue;
    const cb = contrast(rgb, bg), cp = contrast(rgb, panel), worst = Math.min(cb, cp);
    rows.push(h("div", { class: "ui-tok" },
      h("span", { class: "ui-sw", style: `background:var(${name})` }),
      h("b", null, name), h("span", { class: "meta" }, value),
      h("span", { class: "meta" }, "on bg ", cb.toFixed(1), " · on panel ", cp.toFixed(1), " ",
        h("span", { class: worst >= 4.5 ? "ui-good" : worst >= 3 ? "" : "ui-bad" }, worst >= 4.5 ? "AA" : worst >= 3 ? "AA large" : "low"))));
  }
  $("ui-tokens").replaceChildren(...rows);
}

// ---- components ----
function glyphs() {
  const out = [];
  for (const a of [...kit.all().map((x) => x.name), ""]) {
    out.push(h("span", { class: "ui-row" }, h("span", { class: "ui-lab" }, kit.label(a) || "other"),
      kit.glyph(a), kit.glyph(a, { size: "lg" }), kit.glyph(a, { muted: true })));
  }
  $("ui-glyphs").replaceChildren(...out);
}

function chips() {
  $("ui-chips").replaceChildren(...Object.entries(CHIP).map(([st, [label, tone]]) =>
    h("span", { class: "chip chip-" + tone, title: st }, tone === "work" ? h("span", { class: "shine" }, label) : label)));
}

function bars() {
  const out = [];
  for (const [name, fn] of [["progress", progCell], ["context", ctxCell], ["plan usage", useCell]]) {
    for (const f of [0, 0.25, 0.5, 0.75, 1]) out.push(h("span", { class: "ui-lab" }, name + " " + f * 100 + "%"), cellBar(f, fn));
  }
  $("ui-bars").replaceChildren(...out);
}

function controls() {
  const sessions = FIXTURES.map((s) => s);
  $("ui-controls").replaceChildren(
    h("button", { class: "btn btn-primary", type: "button" }, "Launch Codex"),
    h("button", { class: "btn btn-secondary", type: "button" }, "Fleet"),
    h("button", { class: "btn btn-secondary active", type: "button" }, "Active"),
    h("button", { class: "btn btn-secondary", type: "button", "aria-pressed": "true" }, "Pressed"),
    kit.filterControl(sessions, () => controls()),
    h("span", { class: "unk", title: "Codex doesn't report cost" }, "cost " + DASH),
    h("span", { class: "tool tool-unknown" }, "› ", h("span", { class: "tool-name" }, "Bash"), " sleep 9 ", h("span", { class: "mark" }, mark("unknown"))),
    h("span", { class: "tool tool-fail" }, "› ", h("span", { class: "tool-name" }, "Bash"), " go test ", h("span", { class: "mark" }, mark("fail"))));
}

const T = Date.now(), M = 60e3;
const FIXTURES = [
  { agent: "claude", status: "working", name: "trk", task: "Agent adapters", step_text: "Writing tests", step_i: 5, step_n: 8, ctx_pct: 34, cost_usd: 1.84, tokens_in: 180e3, tokens_out: 32e3 },
  { agent: "codex", status: "waiting", name: "trk #9f3a", task: "Fix flaky e2e", step_text: "Re-running on macOS", step_i: 2, step_n: 4, need: { type: "permission", text: "Bash: go test -tags e2e ./e2e/..." } },
  { agent: "claude", status: "blocked", name: "arrow", task: "Port the reader", step_i: 3, step_n: 6, ctx_pct: 52, cost_usd: 4.1 },
  { agent: "codex", status: "looping", name: "vendor", reality: "Bash `npm test` failed 6× in 4 min", step_i: 1, step_n: 3 },
  { agent: "claude", status: "done", name: "docs", task: "README for agents", step_i: 4, step_n: 4, last_reply: "Updated the README and the architecture notes.", ctx_pct: 71, cost_usd: 0.92 },
  { agent: "codex", status: "interrupted", name: "trk", task: "Bump xterm.js", step_i: 1, step_n: 2, last_reply: "" },
  { agent: "", status: "idle", name: "scripts", task: "Nightly backup", step_i: 0, step_n: 0 },
].map((s, i) => ({
  session_id: (s.agent ? s.agent + ":" : "") + "fixture-" + i, started_at: T - (40 - i * 5) * M, last_event_at: T - i * M,
  last_prompt: "Make TRK a generic harness with clean adapter boundaries", last_prompt_at: T - 6 * M,
  recent: [{ tool: s.agent === "codex" ? "apply_patch" : "Edit", summary: "internal/agent/codex/hook.go", state: "ok" }, { tool: "Bash", summary: "go test ./...", state: i % 3 ? "ok" : "fail" }],
  timeline: [{ start: T - 20 * M, end: T - 18 * M, cat: "read" }, { start: T - 17 * M, end: T - 12 * M, cat: "edit" }, { start: T - 11 * M, end: T - 9 * M, cat: "run" }, { start: T - 8 * M, end: T - 6 * M, cat: "wait" }],
  files_read: 6, files_edited: 3, lines_added: 240, lines_removed: 61, api_ms: 9 * M, wall_ms: 14 * M,
  subagents: i % 2 ? { explorer: 2 } : {}, bg_running: i === 0 ? 1 : 0, ctx: { level: "ok", hints: [] }, ...s,
}));

function cardsFx() { $("ui-cards").replaceChildren(...FIXTURES.map((s) => card(s))); }

function timelineFx() {
  const end = T, span = 30 * M;
  const track = h("div", { class: "ui-tl" });
  for (const b of FIXTURES[0].timeline) {
    const l = ((b.start - (end - span)) / span) * 100, r = ((b.end - (end - span)) / span) * 100;
    track.append(h("span", { class: "tl-block tl-" + b.cat, style: `left:${l}%;width:${r - l}%`, title: b.cat }));
  }
  $("ui-timeline").replaceChildren(track, h("div", { class: "tl-legend" },
    h("span", { class: "tl-key tl-read" }), "read ", h("span", { class: "tl-key tl-edit" }), "edit ",
    h("span", { class: "tl-key tl-run" }), "run ", h("span", { class: "tl-key tl-wait" }), "waiting on you"));
}

tokens();
glyphs();
chips();
bars();
controls();
cardsFx();
timelineFx();
