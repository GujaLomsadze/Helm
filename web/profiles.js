"use strict";
// Launch profiles, shared by the Fleet page (New agent form, card badges) and the Usage page:
// the list and presets (GET /v1/profiles), spend per profile (GET /v1/cost), budget state, and
// the add/edit dialog. Builds DOM with createElement/textContent only. Keys are typed here and
// sent once; the daemon never sends them back, only whether each one is set.

window.HelmProfiles = (function () {
  function el(tag, props, ...kids) {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(props || {})) {
      if (v == null || v === false) continue;
      if (k === "class") e.className = v;
      else if (k === "style") e.style.cssText = v;
      else e.setAttribute(k, v);
    }
    for (const kid of kids.flat(Infinity)) {
      if (kid == null || kid === false) continue;
      e.append(kid instanceof Node ? kid : document.createTextNode(String(kid)));
    }
    return e;
  }

  // Phosphor-friendly hues for profiles; "My login" (no profile) stays muted.
  const PALETTE = ["#ff7a6b", "#ffd66b", "#8ccfe0", "#5cff9d", "#c9a0ff", "#ffb27a", "#ff8fc7", "#a8e05f"];
  const HEX = /^#[0-9a-f]{6}$/i;
  const NONE = { id: "", name: "My login", color: "#b9928a" };

  let state = { profiles: [], presets: [] }, loading = null;
  const listeners = new Set();

  async function load(force) {
    if (loading && !force) return loading;
    loading = (async () => {
      try {
        const r = await fetch("/v1/profiles");
        if (r.ok) state = await r.json();
      } catch { /* keep what we had */ }
      for (const f of listeners) f(state);
      return state;
    })();
    return loading;
  }
  const onChange = (f) => listeners.add(f);

  // A snapshot from the stream carries the profiles too: keep the cache current without a fetch.
  function fromSnapshot(list) {
    if (!list) return;
    const sig = JSON.stringify(list);
    if (sig === JSON.stringify(state.profiles)) return;
    state = { ...state, profiles: list };
    for (const f of listeners) f(state);
  }

  const profiles = (agent) => state.profiles.filter((p) => !agent || p.agent === agent);
  const byId = (id) => (id ? state.profiles.find((p) => p.id === id) : NONE);
  const presetsFor = (agent) => (state.presets.find((x) => x.agent === agent) || {}).presets || [];
  const conflictsFor = (agent) => (state.presets.find((x) => x.agent === agent) || {}).conflicts || [];
  const supports = (agent) => presetsFor(agent).length > 0;
  const preset = (p) => presetsFor(p.agent).find((x) => x.id === p.preset);
  const kind = (p) => (p && p.id ? (preset(p) || {}).label || p.preset : "Your own sign-in");
  function color(p) {
    if (!p || !p.id) return NONE.color;
    if (p.color && HEX.test(p.color)) return p.color;
    const i = state.profiles.findIndex((x) => x.id === p.id);
    return PALETTE[(i < 0 ? 0 : i) % PALETTE.length];
  }

  // ---- spend ----
  let costCache = { at: 0, days: 0, data: null };
  async function cost(days = 30, maxAge = 30e3) {
    if (costCache.data && costCache.days === days && Date.now() - costCache.at < maxAge) return costCache.data;
    const r = await fetch("/v1/cost?days=" + days);
    if (!r.ok) throw new Error((await r.text()).trim() || r.status);
    costCache = { at: Date.now(), days, data: await r.json() };
    return costCache.data;
  }

  // Budget state of one /v1/cost profile entry: the gateway's numbers when it gave them,
  // else Helm's estimate over the profile's window. pct is null without a budget.
  function budget(cp) {
    if (!cp) return { spent: 0, budget: 0, pct: null, source: "estimate" };
    const g = cp.gateway;
    if (g) {
      const b = g.budget_usd != null ? g.budget_usd : cp.budget_usd || 0;
      return { spent: g.spend_usd, budget: b, pct: b > 0 ? (g.spend_usd / b) * 100 : null, source: "gateway", resetAt: g.reset_at || 0, checked: g.checked };
    }
    const b = cp.budget_usd || 0;
    return { spent: cp.window_usd || 0, budget: b, pct: b > 0 ? ((cp.window_usd || 0) / b) * 100 : null, source: "estimate",
      error: cp.gateway_error || "", windowDays: cp.budget_days || 0 };
  }
  const tone = (pct) => (pct == null ? "" : pct >= 100 ? "red" : pct >= 80 ? "yellow" : "ok");
  const money = (v) => "$" + (v >= 100 ? Math.round(v).toLocaleString("en") : v.toFixed(2));

  // ---- editor ----
  let dlg = null;
  function edit(existing, opt = {}) {
    if (dlg) dlg.remove();
    const agent = (existing && existing.agent) || opt.agent || (state.presets[0] || {}).agent;
    const presets = presetsFor(agent);
    const dup = !!opt.duplicate;
    const cur = existing ? { ...existing } : { preset: (presets[0] || {}).id, values: {}, extra: {}, budget_days: 30, spend: "local" };
    const isNew = !existing || dup;
    if (dup) { cur.name = existing.name + " (2)"; cur.secrets = {}; }
    const err = el("p", { class: "pf-err", role: "alert" });
    const note = el("div", { class: "pf-note", "aria-live": "polite" });
    const name = el("input", { id: "pf-name", maxlength: "40", autocomplete: "off", spellcheck: "false", value: cur.name || "", placeholder: "Corp" });
    const type = el("select", { id: "pf-type" }, presets.map((p) => el("option", { value: p.id, selected: p.id === cur.preset ? "" : null }, p.label)));
    const hint = el("p", { class: "meta pf-hint" });
    const fields = el("div", { class: "pf-grid" });
    const colors = el("div", { class: "pf-colors", role: "radiogroup", "aria-label": "Colour" });
    let pickColor = cur.color && HEX.test(cur.color) ? cur.color.toLowerCase() : PALETTE[state.profiles.length % PALETTE.length];
    if (existing && !dup && !cur.color) pickColor = color(existing);
    const drawColors = () => colors.replaceChildren(...PALETTE.map((c) => el("button", { type: "button", class: "pf-swatch", role: "radio",
      "aria-checked": String(c === pickColor), "aria-label": "colour " + c, "data-color": c, style: "--c:" + c })));
    drawColors();
    const budgetIn = el("input", { id: "pf-budget", inputmode: "decimal", autocomplete: "off", value: cur.budget_usd ? String(cur.budget_usd) : "", placeholder: "none" });
    const period = el("select", { id: "pf-period" }, [[30, "30 days, rolling"], [7, "7 days, rolling"], [1, "24 hours, rolling"], [0, "since created"]]
      .map(([d, l]) => el("option", { value: d, selected: (cur.budget_days || 0) === d ? "" : null }, l)));
    const spendBox = el("fieldset", { class: "pf-spend" });
    const extra = el("div", { class: "pf-extra" });
    const addExtra = el("button", { type: "button", class: "btn btn-secondary" }, "+ variable");
    const save = el("button", { type: "submit", class: "btn btn-primary" }, isNew ? "Save profile" : "Save");
    const del = !isNew ? el("button", { type: "button", class: "btn btn-secondary pf-del" }, "Delete") : null;
    const copy = !isNew ? el("button", { type: "button", class: "btn btn-secondary" }, "Duplicate with a new key") : null;
    const check = el("button", { type: "button", class: "btn btn-secondary" }, "Check gateway");
    let spend = cur.spend || "local";

    const extraRow = (k = "", v = "") => el("div", { class: "pf-xrow" },
      el("input", { class: "pf-xk", placeholder: "NAME", value: k, autocomplete: "off", spellcheck: "false", "aria-label": "Variable name" }),
      el("input", { class: "pf-xv", placeholder: "value", value: v, autocomplete: "off", spellcheck: "false", "aria-label": "Variable value" }),
      el("button", { type: "button", class: "btn btn-secondary pf-xdel", "aria-label": "Remove variable" }, "✕"));
    for (const [k, v] of Object.entries(cur.extra || {})) extra.append(extraRow(k, v));

    function drawPreset() {
      const p = presets.find((x) => x.id === type.value) || presets[0];
      hint.textContent = p.hint || "";
      hint.hidden = !p.hint;
      fields.replaceChildren(...p.fields.map((f) => {
        const isSet = !dup && existing && existing.secrets && existing.secrets[f.var];
        const id = "pf-f-" + f.var;
        const input = f.secret
          ? el("input", { id, type: "password", autocomplete: "new-password", spellcheck: "false", "data-secret": f.var,
              placeholder: isSet ? "set · leave empty to keep" : f.placeholder || "" })
          : el("input", { id, autocomplete: "off", spellcheck: "false", "data-var": f.var, value: (cur.values || {})[f.var] || "",
              placeholder: f.placeholder || f.default || "", inputmode: f.url ? "url" : null });
        return el("label", { class: "pf-lab", for: id }, el("span", null, f.label, f.required ? " *" : el("span", { class: "ag-opt" }, " optional")), input);
      }));
      check.hidden = p.spend !== "litellm";
      if (p.spend !== "litellm") spend = "local";
      drawSpend(p);
    }
    function drawSpend(p) {
      spendBox.hidden = p.spend !== "litellm";
      spendBox.replaceChildren(el("legend", null, "Spend comes from"),
        ...[["local", "Helm's estimate", "Adds up Claude Code's own cost figures. Works with any gateway."],
          ["litellm", "The gateway (LiteLLM)", "Asks /key/info for this key's real spend and budget, once a minute."]].map(([v, l, d]) =>
          el("label", { class: "pf-radio" }, el("input", { type: "radio", name: "pf-spend", value: v, checked: spend === v ? "" : null }),
            el("span", null, el("b", null, l), el("small", null, d)))));
    }
    drawPreset();

    const conflicts = conflictsFor(agent);
    const body = el("form", { class: "pf-form", novalidate: "" },
      el("div", { class: "pf-grid" },
        el("label", { class: "pf-lab", for: "pf-name" }, el("span", null, "Name *"), name),
        el("label", { class: "pf-lab", for: "pf-type" }, el("span", null, "Type"), type)),
      hint, fields,
      conflicts.length ? el("p", { class: "pf-warn" }, "These settings override any profile, because Claude Code applies them over the environment: ",
        conflicts.join(", "), ". Remove them there for profiles to take effect.") : null,
      el("div", { class: "pf-grid" },
        el("label", { class: "pf-lab", for: "pf-budget" }, el("span", null, "Budget, USD ", el("span", { class: "ag-opt" }, "optional")), budgetIn),
        el("label", { class: "pf-lab", for: "pf-period" }, el("span", null, "Over"), period)),
      spendBox, note,
      el("div", { class: "pf-lab" }, el("span", null, "Colour"), colors),
      el("details", { class: "pf-more", open: Object.keys(cur.extra || {}).length ? "" : null },
        el("summary", null, "Extra variables ", el("span", { class: "ag-opt" }, "ANTHROPIC_MODEL, ANTHROPIC_CUSTOM_HEADERS, …")),
        extra, addExtra),
      el("p", { class: "meta" }, "Keys stay on this machine, in a file only you can read, and are never shown again. Agents started with this profile get these variables instead of your shell's own ANTHROPIC_* settings."),
      err,
      el("div", { class: "pf-actions" }, save, check, copy, del));

    dlg = el("dialog", { class: "panel pf-dlg", "aria-labelledby": "pf-title" },
      el("div", { class: "drawer-head" }, el("h2", { class: "drawer-title", id: "pf-title" }, isNew ? "+ Launch profile" : "Edit " + existing.name),
        el("button", { class: "btn btn-secondary", type: "button", "data-close": "", "aria-label": "Close" }, "✕")),
      body);
    document.body.append(dlg);

    const collect = () => {
      const values = {}, secrets = {}, ex = {};
      for (const i of fields.querySelectorAll("[data-var]")) values[i.dataset.var] = i.value.trim();
      for (const i of fields.querySelectorAll("[data-secret]")) if (i.value.trim()) secrets[i.dataset.secret] = i.value.trim();
      for (const r of extra.querySelectorAll(".pf-xrow")) {
        const k = r.querySelector(".pf-xk").value.trim();
        if (k) ex[k] = r.querySelector(".pf-xv").value;
      }
      const b = budgetIn.value.trim().replace(/^\$/, "");
      return { agent, name: name.value.trim(), preset: type.value, values, secrets, extra: ex, color: pickColor,
        budget_usd: b ? Number(b) : 0, budget_days: Number(period.value), spend };
    };
    const urlField = () => {
      const p = presets.find((x) => x.id === type.value) || {};
      const f = (p.fields || []).find((x) => x.url);
      return f ? (fields.querySelector(`[data-var="${f.var}"]`) || {}).value || "" : "";
    };
    const keyField = () => { const i = fields.querySelector("[data-secret]"); return i ? i.value.trim() : ""; };
    let savedId = isNew ? "" : existing.id;

    async function probe() {
      note.textContent = "Asking the gateway…";
      note.className = "pf-note";
      const r = await fetch("/v1/profiles/probe", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: urlField(), key: keyField(), id: keyField() ? "" : savedId }) });
      if (!r.ok) throw new Error((await r.text()).trim());
      return r.json();
    }
    const showProbe = (res, askToSwitch) => {
      note.replaceChildren();
      if (!res.litellm) {
        note.className = "pf-note pf-note-warn";
        note.textContent = (res.error || "Not a LiteLLM gateway") + ". Helm's estimate still works.";
        return false;
      }
      const s = res.spend;
      note.className = "pf-note pf-note-ok";
      note.append("✓ LiteLLM: this key has spent " + money(s.spend_usd) + (s.budget_usd != null ? " of " + money(s.budget_usd) : " (no limit on the key)") + ". ");
      if (askToSwitch && spend !== "litellm") {
        const yes = el("button", { type: "button", class: "btn btn-secondary" }, "Use the gateway's numbers");
        const no = el("button", { type: "button", class: "btn btn-secondary" }, "Keep Helm's estimate");
        yes.addEventListener("click", async () => {
          spend = "litellm";
          drawSpend(presets.find((x) => x.id === type.value));
          await submit(true);
        });
        no.addEventListener("click", () => close(true));
        note.append(el("span", { class: "pf-ask" }, yes, " ", no));
      }
      return true;
    };

    async function submit(quiet) {
      err.textContent = "";
      const data = collect();
      if (!data.name) { err.textContent = "Give the profile a name."; name.focus(); return false; }
      if (data.budget_usd && !(data.budget_usd > 0)) { err.textContent = "Budget: a number of dollars, or empty."; budgetIn.focus(); return false; }
      save.disabled = true;
      try {
        const r = await fetch(savedId ? "/v1/profiles/" + savedId : "/v1/profiles", { method: savedId ? "PUT" : "POST",
          headers: { "Content-Type": "application/json" }, body: JSON.stringify(data) });
        if (!r.ok) throw new Error((await r.text()).trim());
        const p = await r.json();
        const first = !savedId;
        savedId = p.id;
        await load(true);
        costCache.at = 0;
        if (opt.onSaved) opt.onSaved(p);
        // First save of a gateway profile on Helm's estimate: offer the gateway's own numbers.
        const ps = presets.find((x) => x.id === data.preset) || {};
        if (!quiet && first && ps.spend === "litellm" && spend === "local") {
          try { if (showProbe(await probe(), true)) return true; } catch { /* the estimate is fine */ }
        }
        close(true);
        return true;
      } catch (x) {
        err.textContent = String(x.message || x);
        return false;
      } finally { save.disabled = false; }
    }

    function close() { if (dlg) { dlg.close(); dlg.remove(); dlg = null; } }

    type.addEventListener("change", drawPreset);
    body.addEventListener("submit", (e) => { e.preventDefault(); submit(); });
    body.addEventListener("change", (e) => { if (e.target.name === "pf-spend") spend = e.target.value; });
    colors.addEventListener("click", (e) => { const b = e.target.closest("[data-color]"); if (b) { pickColor = b.dataset.color; drawColors(); } });
    addExtra.addEventListener("click", () => { const r = extraRow(); extra.append(r); r.querySelector("input").focus(); });
    extra.addEventListener("click", (e) => { const b = e.target.closest(".pf-xdel"); if (b) b.parentElement.remove(); });
    check.addEventListener("click", async () => { try { showProbe(await probe(), false); } catch (x) { note.className = "pf-note pf-note-warn"; note.textContent = String(x.message || x); } });
    if (copy) copy.addEventListener("click", () => edit(existing, { ...opt, duplicate: true }));
    if (del) {
      let armed = false;
      del.addEventListener("click", async () => {
        if (!armed) { armed = true; del.textContent = "Click again to delete"; return; }
        try {
          const r = await fetch("/v1/profiles/" + existing.id, { method: "DELETE" });
          if (!r.ok) throw new Error((await r.text()).trim());
          await load(true);
          costCache.at = 0;
          if (opt.onSaved) opt.onSaved(null);
          close();
        } catch (x) { err.textContent = String(x.message || x); }
      });
    }
    dlg.addEventListener("click", (e) => { if (e.target.closest("[data-close]") || e.target === dlg) close(); });
    dlg.addEventListener("cancel", (e) => { e.preventDefault(); close(); });
    dlg.showModal();
    setTimeout(() => (isNew ? name : fields.querySelector("input") || name).focus(), 30);
  }

  // A small coloured badge naming a profile, for cards and lists.
  const badge = (p) => el("span", { class: "prof-badge one", style: "--c:" + color(p), title: p.name + " · " + kind(p) }, p.name);
  const dot = (p) => el("span", { class: "prof-dot", style: "--c:" + color(p), "aria-hidden": "true" });

  return { load, onChange, fromSnapshot, profiles, byId, presetsFor, supports, kind, color, cost, budget, tone, money, edit, badge, dot, NONE };
})();
