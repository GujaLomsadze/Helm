"use strict";
// Agent drawer: click a card (or "+ Agent") to see that agent's terminal, resume or fork a
// session, or start an agent in a folder. Uses h/$/view/dur/now/CHIP/QUIET/kit from app.js
// and HelmTerm from term.js. Only the terminal on screen holds a websocket: the browser
// allows 6 connections per host.

window.HelmAgents = (function () {
  const dlg = $("agents"), tabs = $("ag-tabs"), main = $("ag-main"), grip = $("ag-grip");
  // selected: "new", a terminal id, or "s:<session id>" for a session not running in Helm
  let terms = [], selected = "new", newFrom = null, viewer = null, popped = new Set();
  let acts = {}; // data-act key → handler for the session panel on screen

  const running = () => terms.filter((t) => !t.exited).length;
  const find = (id) => terms.find((t) => t.id === id);
  const session = (sid) => ((view && view.sessions) || []).find((s) => s.session_id === sid);
  const enabled = () => !!(view && view.terminals_enabled && view.terminals_supported);
  const liveTermFor = (sid) => terms.find((t) => !t.exited && t.session_id === sid);
  const termName = (t) => { const s = session(t.session_id); return s ? s.name : t.title || t.name; };
  // A session is resumable when its agent gave it a real id (not a cwd-… fallback or a `hlm start` id).
  const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  const asSession = (x) => (x && x.session_id ? { ...x, agent: x.agent || (session(x.session_id) || {}).agent } : x || {});
  const resumable = (x) => uuid.test(kit.native(asSession(x)) || "");
  const agentOf = (x) => asSession(x).agent || (kit.fallback() || {}).name;
  const agentLabel = (x) => kit.label(agentOf(x));
  // spec for resuming or forking: the daemon wants the agent and its own session id
  const resumeSpec = (x, dir, fork) => ({ agent: agentOf(x), dir, resume: kit.native(asSession(x)), fork: fork || undefined });
  const shq = (s) => "'" + String(s).replace(/'/g, "'\\''") + "'";
  const resumeCmd = (s) => `cd ${shq(s.cwd)} && ${kit.resumeCommand(asSession(s))}`;

  // header button + power-button wording follow every snapshot
  function update(v) {
    terms = v.terminals || [];
    const btn = $("agent-open"), n = running();
    btn.hidden = !enabled() && !terms.length;
    btn.replaceChildren(...["+ Agent", n ? h("span", { class: "ag-count" }, n) : null].filter(Boolean));
    btn.title = n ? `${n} agent${n === 1 ? "" : "s"} running in Helm` : "Start an agent in a terminal";
    if (!dlg.open) return;
    if (selected.startsWith("s:")) {
      const sid = selected.slice(2), t = liveTermFor(sid);
      if (t) return show(t.id); // it started running in Helm
      if (!session(sid)) return show("new");
      drawTabs();
      drawActions();
      return drawSession(sid);
    }
    if (selected !== "new" && !find(selected)) return show("new"); // stopped elsewhere
    drawTabs();
    drawActions();
  }

  function drawTabs() {
    const tab = (id, label, sub, cls, here) => h("div", { class: "ag-tab-wrap" },
      h("button", { class: "ag-tab " + cls, type: "button", "data-tab": id, "aria-pressed": String(selected === id) },
        h("span", { class: "one" }, label), sub ? h("span", { class: "ag-sub one" }, sub) : null),
      here ? h("button", { class: "ag-here", type: "button", "data-here": id, title: "New agent in " + here, "aria-label": "New agent in " + here }, "+") : null);
    const list = [tab("new", "+ New agent", enabled() ? "" : "off in ⚙", "ag-new")];
    for (const t of terms) {
      list.push(tab(t.id, termName(t), (t.exited ? "exited · " : "● ") + dur(now() - t.started) + (t.fork ? " · fork" : ""),
        t.exited ? "ag-exited" : "ag-live", enabled() ? t.dir : ""));
    }
    if (selected.startsWith("s:")) {
      const s = session(selected.slice(2));
      if (s) list.push(tab(selected, s.name, view.running && s.session_id in view.running ? "▭ another terminal"
        : QUIET.includes(s.status) ? "not running" : s.status, "ag-ext"));
    }
    tabs.replaceChildren(...list);
  }

  function drawActions() {
    const t = find(selected), s = selected.startsWith("s:") ? session(selected.slice(2)) : null;
    $("ag-pop").hidden = !t || t.exited;
    $("ag-font").hidden = !t || popped.has(t.id);
    $("ag-stop").hidden = !t;
    $("ag-stop").textContent = t && t.exited ? "Remove" : "Stop " + agentLabel(t || {});
    $("ag-resume").hidden = !(t && t.exited && resumable(t) && enabled());
    $("agents-title").textContent = t ? termName(t) : s ? s.name : "+ New agent";
    if (!renaming) $("ag-rename").hidden = !session(currentSid());
  }

  // ---- rename: ✎ swaps the title for a field; Enter saves, Esc or leaving cancels, empty restores ----
  let renaming = false;
  const currentSid = () => selected.startsWith("s:") ? selected.slice(2) : (find(selected) || {}).session_id || "";
  function startRename() {
    const s = session(currentSid());
    if (!s) return;
    renaming = true;
    const input = $("ag-title-input");
    input.value = s.title || "";
    input.placeholder = s.auto_name || s.name;
    $("ag-title-hint").textContent = "Enter saves · Esc cancels · empty = " + (s.auto_name || s.name);
    $("agents-title").hidden = $("ag-rename").hidden = true;
    $("ag-title-form").hidden = false;
    input.focus();
    input.select();
  }
  function endRename() {
    renaming = false;
    $("ag-title-form").hidden = true;
    $("agents-title").hidden = false;
    drawActions();
  }
  $("ag-rename").addEventListener("click", startRename);
  $("ag-title-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const sid = currentSid(), title = $("ag-title-input").value;
    try {
      const r = await fetch("/v1/sessions/" + encodeURIComponent(sid) + "/title", { method: "POST",
        headers: { "Content-Type": "application/json" }, body: JSON.stringify({ title }) });
      if (!r.ok) throw new Error((await r.text()).trim());
      const s = session(sid); // show it now; the next snapshot confirms
      if (s) { s.title = title.trim(); if (s.title) { s.auto_name = s.auto_name || s.name; s.name = s.title; } else if (s.auto_name) s.name = s.auto_name; }
      endRename();
      drawTabs();
    } catch (x) { $("ag-title-hint").textContent = String(x.message || x); }
  });
  $("ag-title-input").addEventListener("keydown", (e) => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); endRename(); } });
  $("ag-title-input").addEventListener("blur", () => setTimeout(() => { if (renaming && document.activeElement !== $("ag-title-input")) endRename(); }, 150));

  const AGENT_KEY = "hlm.newAgent";
  function lastAgent() { try { return localStorage.getItem(AGENT_KEY) || ""; } catch { return ""; } }
  function setLastAgent(a) { try { localStorage.setItem(AGENT_KEY, a); } catch { /* per-browser convenience */ } }
  const multiAgents = () => kit.multi((view && view.sessions) || []);

  function dropViewer() { if (viewer) { viewer.dispose(); viewer = null; } }

  function show(id, from) {
    if (renaming) endRename();
    dropViewer();
    selected = id;
    newFrom = id === "new" ? from || null : null;
    drawTabs();
    drawActions();
    if (id.startsWith("s:")) return drawSession(id.slice(2), true);
    const t = find(id);
    if (!t) return main.replaceChildren(form(newFrom));
    if (popped.has(id)) {
      return main.replaceChildren(h("div", { class: "ag-note" }, h("b", null, "Open in its own window"),
        h("button", { class: "btn btn-secondary", type: "button", "data-unpop": id }, "Show it here instead")));
    }
    const el = h("div", { class: "ag-term" });
    main.replaceChildren(h("div", { class: "ag-meta one", title: t.dir + (t.prompt ? " · " + t.prompt : "") }, t.dir,
      t.fork ? " · forked conversation" : t.resume ? " · resumed" : "", t.prompt ? " · “" + t.prompt + "”" : ""), el);
    viewer = HelmTerm.attach(el, id, { onExit: () => update(view || {}) });
    setTimeout(() => viewer && viewer.focus(), 50);
  }

  // A session that isn't running in a Helm terminal: it runs in another terminal, or nowhere.
  let sessionSig = "";
  function drawSession(sid, force) {
    const s = session(sid);
    if (!s) return show("new");
    const elsewhere = !!(view.running && sid in view.running);
    // busy, but Helm can't see a process for it (an agent without PID tracking, or one it hasn't linked yet)
    const maybe = !elsewhere && !QUIET.includes(s.status);
    const who = agentLabel(s), cmd = kit.resumeCommand(asSession(s));
    const on = enabled();
    const sig = JSON.stringify([sid, elsewhere, on, s.last_prompt, s.status]);
    if (!force && sig === sessionSig) return; // snapshots arrive every few seconds; keep the panel still
    sessionSig = sig;
    acts = {};
    const err = h("span", { class: "ag-err", role: "alert" });
    const act = (key, name, desc, cmd, run, opt = {}) => {
      acts[key] = run;
      return h("button", { class: "ag-act" + (opt.primary ? " primary" : ""), type: "button", "data-act": key,
        "aria-disabled": opt.disabled ? "true" : null },
        h("span", { class: "ag-act-n" }, name), h("span", { class: "ag-act-d" }, opt.why || desc), cmd ? h("code", null, cmd) : null);
    };
    const launchSpec = (spec) => async (b) => {
      b.setAttribute("aria-disabled", "true");
      err.textContent = "";
      try { await launch(spec); } catch (x) { err.textContent = String(x.message || x); b.removeAttribute("aria-disabled"); }
    };
    const folder = s.cwd.split("/").filter(Boolean).pop() || s.cwd;
    const resume = act("resume", "↻ " + (elsewhere || maybe ? "Take over" : "Resume"),
      maybe ? `Only if ${who} has stopped working on it: two agents on one conversation both write to it.` : "Continue this conversation in a Helm terminal.",
      cmd, launchSpec(resumeSpec(s, s.cwd)),
      { primary: !elsewhere && !maybe, disabled: !on || elsewhere, why: elsewhere ? `Still running in another terminal. Stop ${who} there first, or fork it.` : "" });
    const fork = act("fork", "↳ Fork into Helm", "A copy of the conversation runs here; the original stays as it is.", null,
      launchSpec(resumeSpec(s, s.cwd, true)), { primary: elsewhere || maybe, disabled: !on });
    const fresh = act("new", "+ New agent here", `Fresh ${who} in ${folder}/, nothing carried over.`, null,
      () => show("new", { dir: s.cwd, name: s.name, agent: agentOf(s) }), { disabled: !on });
    const copy = act("copy", "» Copy resume command", "Paste it into any terminal.", resumeCmd(s), (b) => copyText(resumeCmd(s), b));
    const off = !view.terminals_supported ? "Terminals don't run on this system (native Windows); copy the command instead."
      : !on ? "Terminals are off. Turn them on in ⚙ Card layout → Terminals." : "";
    main.replaceChildren(h("div", { class: "ag-ext" },
      h("p", { class: "ag-intro" }, !resumable(s)
        ? "Helm knows this session by its folder only, so it can't resume it. You can start a fresh agent there."
        : elsewhere ? `${who} is running this session in another terminal (a tab or your editor). Helm can watch it, but can't show or type into that window.`
        : maybe ? `This session looks busy, but Helm can't see where ${who} runs it. Fork it to be safe, or take it over if it has stopped.`
        : "This session isn't running anywhere right now. Pick it up where it stopped."),
      h("dl", { class: "ag-where" },
        h("dt", null, "folder"), h("dd", null, s.cwd || DASH),
        h("dt", null, "agent"), h("dd", null, multiAgents() ? kit.glyph(agentOf(s)) : null, multiAgents() ? " " : null, who),
        h("dt", null, "session"), h("dd", null, kit.native(asSession(s)) || sid),
        h("dt", null, "status"), h("dd", null, (CHIP[s.status] || [s.status])[0].toLowerCase(), " · last ",
          h("span", { class: "live-ago", "data-since": s.last_event_at }, dur(now() - s.last_event_at) + " ago"))),
      s.last_prompt ? h("div", { class: "w-prompt" }, h("div", { class: "w-who" }, "Last prompt"), h("div", { class: "w-txt clamp2" }, s.last_prompt)) : null,
      resumable(s)
        ? h("div", { class: "ag-acts" }, elsewhere || maybe ? [fork, resume, fresh, copy] : [resume, fork, fresh, copy])
        : h("div", { class: "ag-acts" }, fresh),
      off ? h("p", { class: "meta" }, off) : null, err));
  }

  function form(from) {
    const seen = new Set(), dirs = [];
    for (const s of [...((view && view.sessions) || [])].sort((a, b) => b.last_event_at - a.last_event_at)) {
      if (s.cwd && !seen.has(s.cwd)) { seen.add(s.cwd); dirs.push(s.cwd); }
    }
    const on = enabled();
    const err = h("span", { class: "ag-err", role: "alert" });
    const dir = h("input", { id: "ag-dir", list: "ag-dirs", autocomplete: "off", spellcheck: "false", value: (from && from.dir) || dirs[0] || "", placeholder: "/path/to/project" });
    const base = (d) => d.split("/").filter(Boolean).pop() || "";
    const name = h("input", { id: "ag-name", maxlength: "80", autocomplete: "off", spellcheck: "false", placeholder: base(dir.value) || "the folder's name" });
    dir.addEventListener("input", () => { name.placeholder = base(dir.value) || "the folder's name"; });
    const prompt = h("textarea", { id: "ag-prompt", placeholder: "optional, e.g. run the tests and fix what fails" });
    // Which agent: offered only when more than one can be started here; the last pick is remembered.
    const startable = kit.all().filter((a) => a.available);
    let pick = (from && from.agent) || lastAgent();
    if (!startable.some((a) => a.name === pick)) pick = (startable[0] || kit.fallback() || {}).name;
    const go = h("button", { class: "btn btn-primary", type: "submit", disabled: on ? null : "" }, "Launch " + kit.label(pick));
    const chooser = startable.length > 1 ? h("div", { class: "ag-pick", role: "group", "aria-label": "Agent" }, startable.map((a) =>
      h("button", { type: "button", "data-agent": a.name, "aria-pressed": String(a.name === pick) },
        h("b", null, kit.glyph(a.name, { size: "lg" }), a.label), h("span", null, a.counters ? "cost and tokens on its card" : "activity only; usage on Limits")))) : null;
    const f = h("form", { class: "ag-form" },
      from ? h("div", { class: "ag-from" }, "Folder from " + from.name) : null,
      chooser ? [h("span", { class: "ag-lab" }, "Agent"), chooser] : null,
      h("label", { class: "ag-lab", for: "ag-dir" }, "Folder"), dir,
      h("datalist", { id: "ag-dirs" }, dirs.map((d) => h("option", { value: d }))),
      dirs.length ? h("div", { class: "ag-recent" }, dirs.slice(0, 8).map((d) =>
        h("button", { type: "button", "data-dir": d, title: d }, d.split("/").filter(Boolean).pop() || d))) : null,
      h("label", { class: "ag-lab", for: "ag-name" }, "Name ", h("span", { class: "ag-opt" }, "optional · shown on its card")), name,
      h("label", { class: "ag-lab", for: "ag-prompt" }, "First prompt"), prompt,
      h("div", { class: "ag-go" }, go, err),
      h("p", { class: "meta" }, on
        ? "Runs the agent in a terminal owned by Helm. Closing this drawer keeps it running. If Helm restarts, resume it from its card."
        : view && !view.terminals_supported ? "Terminals don't run on this system (native Windows)."
        : "Terminals are off. Turn them on in ⚙ Card layout → Terminals."));
    f.addEventListener("click", (e) => {
      const b = e.target.closest("[data-dir]");
      if (b) { dir.value = b.dataset.dir; name.placeholder = base(dir.value); }
      const a = e.target.closest("[data-agent]");
      if (a) {
        pick = a.dataset.agent;
        setLastAgent(pick);
        for (const x of f.querySelectorAll("[data-agent]")) x.setAttribute("aria-pressed", String(x === a));
        go.textContent = "Launch " + kit.label(pick);
      }
    });
    f.addEventListener("submit", async (e) => {
      e.preventDefault();
      err.textContent = "";
      go.disabled = true;
      try { await launch({ agent: pick, dir: dir.value.trim(), prompt: prompt.value, title: name.value }); }
      catch (x) { err.textContent = String(x.message || x); go.disabled = false; }
    });
    if (from) setTimeout(() => prompt.focus(), 50);
    return f;
  }

  async function launch(spec) {
    const r = await fetch("/v1/terms", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(spec) });
    if (!r.ok) throw new Error((await r.text()).trim());
    const t = await r.json();
    // a resume replaces the exited terminal of that session (the daemon drops it too)
    terms = [...terms.filter((x) => !(spec.resume && !spec.fork && x.exited && x.agent === t.agent && kit.native(asSession(x)) === spec.resume)), t];
    show(t.id);
  }

  async function copyText(text, b) {
    const say = (msg) => { const d = b.querySelector(".ag-act-d"); if (d) d.textContent = msg; };
    try { await navigator.clipboard.writeText(text); say("Copied."); }
    catch { say("Couldn't copy; select the command below."); }
  }

  // ---- width: drag the left edge (or focus it and use ←/→); remembered in this browser ----
  const W_KEY = "hlm.agentDrawerW", W_MIN = 380;
  const clampW = (w) => Math.round(Math.max(Math.min(W_MIN, innerWidth), Math.min(innerWidth - 24, w)));
  function setW(w, save) {
    dlg.style.setProperty("--ag-w", clampW(w) + "px");
    if (save) try { localStorage.setItem(W_KEY, String(clampW(w))); } catch { /* storage off */ }
  }
  function loadW() {
    let w = 0;
    try { w = Number(localStorage.getItem(W_KEY)) || 0; } catch { /* storage off */ }
    if (w) setW(w); else dlg.style.removeProperty("--ag-w");
  }
  grip.addEventListener("pointerdown", (e) => {
    e.preventDefault();
    grip.setPointerCapture(e.pointerId);
    dlg.classList.add("resizing");
    const move = (ev) => setW(innerWidth - ev.clientX);
    const up = () => {
      grip.removeEventListener("pointermove", move);
      dlg.classList.remove("resizing");
      setW(dlg.getBoundingClientRect().width, true);
    };
    grip.addEventListener("pointermove", move);
    grip.addEventListener("pointerup", up, { once: true });
    grip.addEventListener("pointercancel", up, { once: true });
  });
  grip.addEventListener("dblclick", () => { try { localStorage.removeItem(W_KEY); } catch { /* storage off */ } dlg.style.removeProperty("--ag-w"); });
  grip.addEventListener("keydown", (e) => {
    const step = e.shiftKey ? 120 : 40, w = dlg.getBoundingClientRect().width;
    if (e.key === "ArrowLeft") { setW(w + step, true); e.preventDefault(); }
    if (e.key === "ArrowRight") { setW(w - step, true); e.preventDefault(); }
  });

  // ---- open with a CRT power-on: a bright line that opens into the picture ----
  function open(id, from) {
    if (!dlg.open) {
      loadW();
      dlg.showModal();
      if (!matchMedia("(prefers-reduced-motion: reduce)").matches) {
        dlg.animate([
          { clipPath: "inset(50% 0 50% 0)", filter: "brightness(3)" },
          { clipPath: "inset(49.5% 0 49.5% 0)", filter: "brightness(3)", offset: 0.3 },
          { clipPath: "inset(0 0 0 0)", filter: "brightness(1.4)", offset: 0.75 },
          { clipPath: "inset(0 0 0 0)", filter: "brightness(1)" }], { duration: 420, easing: "cubic-bezier(.2,.8,.2,1)" });
      }
    }
    show(id, from);
  }

  // A card click opens that session: its Helm terminal when it has one, else what can be done with it.
  function openSession(sid) {
    const elsewhere = !!(view && view.running && view.running[sid] === "");
    const t = liveTermFor(sid) || (!elsewhere && terms.find((x) => x.session_id === sid && x.exited));
    open(t ? t.id : "s:" + sid);
  }

  tabs.addEventListener("click", (e) => {
    const here = e.target.closest("[data-here]");
    if (here) { const t = find(here.dataset.here); return t && show("new", { dir: t.dir, name: termName(t) }); }
    const b = e.target.closest("[data-tab]");
    if (b) show(b.dataset.tab);
  });
  main.addEventListener("click", (e) => {
    const u = e.target.closest("[data-unpop]");
    if (u) { popped.delete(u.dataset.unpop); return show(u.dataset.unpop); }
    const a = e.target.closest("[data-act]");
    if (a && a.getAttribute("aria-disabled") !== "true" && acts[a.dataset.act]) acts[a.dataset.act](a);
  });
  $("ag-pop").addEventListener("click", () => { const t = find(selected); if (!t) return; if (HelmTerm.popOut(t)) { popped.add(t.id); show(t.id); } });
  $("ag-resume").addEventListener("click", async () => {
    const t = find(selected);
    if (!t) return;
    try { await launch(resumeSpec(t, t.dir)); } catch (x) { alert(String(x.message || x)); }
  });
  $("ag-stop").addEventListener("click", async () => {
    const t = find(selected);
    if (!t || (!t.exited && !confirm(`Stop ${agentLabel(t)} in ${termName(t)}?\n\nThe agent is killed; you can resume its conversation from its card.`))) return;
    dropViewer();
    await HelmTerm.stop(t.id);
    terms = terms.filter((x) => x.id !== t.id);
    show("new");
  });
  HelmTerm.fontControl($("ag-font"));
  $("ag-close").addEventListener("click", () => dlg.close());
  dlg.addEventListener("click", (e) => { if (e.target === dlg) dlg.close(); }); // backdrop
  dlg.addEventListener("close", dropViewer); // closing never stops an agent
  // "+ Agent" always starts on the New agent form; running agents stay one click away in the tabs
  $("agent-open").addEventListener("click", () => open("new"));

  return { update, running, openSession };
})();
