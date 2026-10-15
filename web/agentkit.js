"use strict";
// The agents Helm knows (GET /v1/agents), their glyphs, and the agent filter shared by
// Fleet and Stats. Nothing here names an agent: names, labels, icons and abilities all
// come from the daemon. Until a second agent has been used, every page looks as it did
// with Claude Code alone: no glyphs, no filter.
// Helm was called TRK: carry this browser's saved settings over once (trk.* → hlm.*).
try {
  for (let i = 0; i < localStorage.length; i++) {
    const k = localStorage.key(i);
    if (k && k.startsWith("trk.") && localStorage.getItem("hlm." + k.slice(4)) == null) localStorage.setItem("hlm." + k.slice(4), localStorage.getItem(k));
  }
} catch { /* storage off */ }

window.HelmAgentKit = (() => {
  let list = [];
  let loading = null;
  const FILTER_KEY = "hlm.agentFilter";

  function load() {
    loading = fetch("/v1/agents")
      .then((r) => (r.ok ? r.json() : list))
      .then((a) => { list = Array.isArray(a) ? a : []; return list; })
      .catch(() => list);
    return loading;
  }
  const ready = () => loading || load();
  const get = (name) => list.find((a) => a.name === name);
  const fallback = () => list[0] || null; // the daemon lists the default agent first
  const label = (name) => (get(name) || {}).label || (name ? name : "Agent");
  // Sessions only reporting through `hlm step` have no agent; they count as "other".
  const key = (s) => s.agent || "";

  // multi: a second agent is in use, by its sessions now or in the last 30 days.
  function multi(sessions) {
    const names = new Set(list.filter((a) => a.seen).map((a) => a.name));
    for (const s of sessions || []) names.add(key(s));
    names.delete("");
    return names.size > 1;
  }

  // glyph: the agent's mark as a CSS mask, so it takes the theme's colour for that agent.
  function glyph(name, opts = {}) {
    const a = get(name);
    const el = document.createElement("span");
    el.className = "glyph" + (opts.muted ? " glyph-muted" : "") + (opts.size ? " glyph-" + opts.size : "");
    el.setAttribute("role", "img");
    el.setAttribute("aria-label", label(name));
    el.title = label(name);
    if (a && a.icon) {
      el.style.setProperty("--ico", `url("icons/${encodeURIComponent(a.name)}.svg")`);
      el.style.setProperty("--gc", `var(--agent-${a.name}, var(--agent-other))`);
    } else {
      el.classList.add("glyph-text");
      el.textContent = ">_";
    }
    return el;
  }

  // Native id and resume command for a session (or a terminal's session key).
  function native(s) {
    if (s.native_id) return s.native_id;
    const a = s.agent && get(s.agent);
    const sid = s.session_id || "";
    return a && a !== fallback() && sid.startsWith(a.name + ":") ? sid.slice(a.name.length + 1) : sid;
  }
  function resumeCommand(s) {
    const a = get(s.agent) || fallback();
    return a ? a.resume.replace("{id}", native(s)) : "";
  }

  // ---- filter: "" = all agents ----
  function filter() { try { return localStorage.getItem(FILTER_KEY) || ""; } catch { return ""; } }
  function setFilter(v) { try { v ? localStorage.setItem(FILTER_KEY, v) : localStorage.removeItem(FILTER_KEY); } catch { /* per-browser convenience */ } }
  const keep = (s) => { const f = filter(); return !f || key(s) === f || (f === "other" && !key(s)); };

  // filterControl: ALL · <agent> n · … ; counts from sessions (omit to show none).
  function filterControl(sessions, onChange, cls = "agent-seg") {
    const f = filter();
    const names = list.filter((a) => a.seen || (sessions || []).some((s) => key(s) === a.name)).map((a) => a.name);
    const others = (sessions || []).some((s) => !key(s));
    const count = (n) => sessions ? (sessions || []).filter((s) => (n === "other" ? !key(s) : key(s) === n)).length : null;
    const btn = (v, kids, title) => {
      const b = document.createElement("button");
      b.type = "button";
      b.dataset.agent = v;
      b.setAttribute("aria-pressed", String(f === v));
      if (title) b.title = title;
      b.append(...kids);
      return b;
    };
    const num = (n) => { if (n == null) return null; const s = document.createElement("span"); s.className = "n"; s.textContent = n; return s; };
    const wrap = document.createElement("div");
    wrap.className = cls;
    wrap.setAttribute("role", "group");
    wrap.setAttribute("aria-label", "Show agents");
    wrap.append(btn("", ["All"].concat(num(sessions ? sessions.length : null) || [])));
    const text = (t) => { const s = document.createElement("span"); s.className = "lbl"; s.textContent = t; return s; };
    for (const n of names) wrap.append(btn(n, [glyph(n), text(label(n))].concat(num(count(n)) || []), "Show only " + label(n)));
    if (others) wrap.append(btn("other", [glyph(""), text("Other")].concat(num(count("other")) || []), "Sessions that only report progress (hlm step)"));
    wrap.addEventListener("click", (e) => {
      const b = e.target.closest("button[data-agent]");
      if (!b) return;
      setFilter(b.dataset.agent);
      onChange(b.dataset.agent);
    });
    return wrap;
  }

  // set: fixture agents (ui.html renders components without a daemon).
  function set(a) { list = a; loading = Promise.resolve(list); }

  return { set, load, ready, get, label, multi, glyph, native, resumeCommand, filter, setFilter, keep, filterControl, all: () => list, fallback };
})();
