"use strict";
// Sound effects for agent status changes (picked sketch: docs/sketches/sounds.html, "Glass" pack).
// Tones are synthesized with Web Audio: no sound files. Sounds always play unless muted (header
// button, or per card); bursts within 1.5 s coalesce into the most urgent one.
// Cards already animate per status; app.js keeps the tab title and icon current.
window.HelmSounds = (function () {
  const KEY = "helm.sounds";
  const COALESCE_MS = 1500;
  const BUDGET_STEPS = [80, 100]; // percent of a launch profile's budget
  const PRIO = ["blocked", "budget", "waiting", "done"];

  // Glass: sine bells with a long tail. [Hz, start s, length s, gain]
  const GLASS = {
    done: [[1047, 0, 0.9, 0.5], [1319, 0.09, 0.9, 0.45], [1568, 0.18, 1.1, 0.4]],
    waiting: [[1319, 0, 0.7, 0.5], [1047, 0.22, 0.9, 0.5]],
    blocked: [[587, 0, 0.8, 0.45], [622, 0, 0.8, 0.45]],
    budget: [[784, 0, 0.6, 0.45], [740, 0.2, 0.6, 0.45], [698, 0.4, 0.9, 0.45]],
  };

  let cfg = { on: true, muted: [] };
  try { cfg = { ...cfg, ...JSON.parse(localStorage.getItem(KEY) || "{}") }; } catch { /* keep defaults */ }
  if (!Array.isArray(cfg.muted)) cfg.muted = [];
  const save = () => { try { localStorage.setItem(KEY, JSON.stringify(cfg)); } catch { /* private window */ } };

  // ---- audio ----
  let ctx = null;
  function audio() {
    if (!ctx) {
      const AC = window.AudioContext || window.webkitAudioContext;
      if (!AC) return null;
      ctx = new AC();
      ctx.onstatechange = paint;
    }
    return ctx;
  }
  // Browsers start audio suspended until the page gets a click or key press.
  const unlocked = () => !!ctx && ctx.state === "running";
  function unlock() {
    const c = audio();
    if (c && c.state === "suspended") c.resume().catch(() => {});
  }
  addEventListener("pointerdown", unlock, true);
  addEventListener("keydown", unlock, true);

  function play(ev) {
    const c = audio(), notes = GLASS[ev];
    if (!c || !notes || c.state !== "running") return;
    const t0 = c.currentTime + 0.02;
    for (const [f, t, d, g] of notes) {
      const at = t0 + t, env = c.createGain(), osc = c.createOscillator();
      env.gain.setValueAtTime(0.0001, at);
      env.gain.exponentialRampToValueAtTime(g * 0.3, at + 0.005);
      env.gain.exponentialRampToValueAtTime(0.0001, at + d);
      osc.type = "sine";
      osc.frequency.setValueAtTime(f, at);
      osc.connect(env).connect(c.destination);
      osc.start(at);
      osc.stop(at + d + 0.05);
    }
  }

  // ---- when to make noise ----
  let pending = [], timer = 0;

  function cue(ev, sid) {
    if (sid && isMuted(sid)) return;
    pending.push(ev);
    clearTimeout(timer);
    timer = setTimeout(() => {
      const best = pending.sort((a, b) => PRIO.indexOf(a) - PRIO.indexOf(b))[0];
      pending = [];
      if (cfg.on) play(best);
    }, COALESCE_MS);
  }

  // ---- status transitions ----
  let prev = null; // session_id -> status; null until the first snapshot seeds it
  function observe(sessions) {
    const next = new Map(sessions.map((s) => [s.session_id, s.status]));
    if (prev) {
      for (const [sid, st] of next) {
        const was = prev.get(sid);
        if (was === undefined || was === st) continue; // new cards and repeats stay quiet
        if (st === "done") cue("done", sid);
        else if (st === "waiting") cue("waiting", sid);
        else if (st === "blocked") cue("blocked", sid);
      }
    }
    prev = next;
  }

  // Budgets live on /v1/cost, not in the snapshot: check once a minute, cue when a
  // profile crosses 80% or 100% of its budget.
  let budgetSeen = null; // profile id -> highest step reached
  async function checkBudgets() {
    const PF = window.HelmProfiles;
    if (!PF || !PF.profiles().length) return;
    let data;
    try { data = await PF.cost(30, 55e3); } catch { return; }
    const now = new Map();
    for (const cp of data.profiles || []) {
      const p = PF.budget(cp).pct;
      now.set(cp.id, p == null ? 0 : BUDGET_STEPS.filter((s) => p >= s).pop() || 0);
    }
    if (budgetSeen) for (const [id, step] of now) if (step > (budgetSeen.get(id) || 0)) cue("budget", "");
    budgetSeen = now;
  }
  setInterval(checkBudgets, 60e3);
  setTimeout(checkBudgets, 3e3);

  // ---- mutes ----
  function isMuted(sid) { return cfg.muted.includes(sid); }
  function toggleCard(sid) {
    cfg.muted = isMuted(sid) ? cfg.muted.filter((x) => x !== sid) : [...cfg.muted, sid].slice(-200);
    save();
  }

  // ---- header toggle ----
  let btn = null;
  function paint() {
    if (!btn) return;
    const locked = cfg.on && !unlocked();
    btn.classList.toggle("snd-off", !cfg.on);
    btn.classList.toggle("snd-locked", locked);
    btn.setAttribute("aria-pressed", String(cfg.on));
    btn.title = !cfg.on ? "Sounds off. Click to turn on"
      : locked ? "Sounds on, waiting for a click anywhere on the page (browsers block audio until then)"
        : "Sounds on: a chime when an agent is done, waits on you, is blocked or nears its budget. Click to mute";
  }
  function mount(el) {
    btn = el;
    btn.addEventListener("click", () => {
      cfg.on = !cfg.on;
      save();
      paint();
      const c = audio(); // a sample when turning on, so you know what to listen for
      if (cfg.on && c) c.resume().then(() => play("done"), () => {});
    });
    paint();
  }

  return { observe, isMuted, toggleCard, mount, play };
})();
