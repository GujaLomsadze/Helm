"use strict";
// Profiles page (profiles.html): every launch profile in a list, the chosen one in full on the
// right — its settings, its keys (masked; Show for 30 s, Copy, Replace), budget and use.
// Keys come from POST /v1/profiles/{id}/reveal one at a time, only when asked, and are
// dropped from the page when they hide. Builds DOM with createElement/textContent only.

(function () {
  const PF = window.HelmProfiles, kit = window.HelmAgentKit;
  const $ = (id) => document.getElementById(id);
  const SHOW_SECONDS = 30;

  function h(tag, props, ...kids) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(props || {})) {
      if (v == null || v === false) continue;
      if (k === "class") el.className = v;
      else el.setAttribute(k, v);
    }
    for (const kid of kids.flat(Infinity)) {
      if (kid == null || kid === false) continue;
      el.append(kid instanceof Node ? kid : document.createTextNode(String(kid)));
    }
    return el;
  }

  let sel = decodeURIComponent(location.hash.slice(1)) || "";
  let cost = null, failed = false;
  let shown = null; // { pid, v, value, left, timer } — one key on screen at a time
  let replacing = ""; // var being replaced on the selected profile
  const probes = {}; // pid → { ok, text }

  function toast(text, warn) {
    const t = $("pp-toast");
    t.textContent = text;
    t.classList.toggle("warn", !!warn);
    t.hidden = false;
    clearTimeout(toast.timer);
    toast.timer = setTimeout(() => { t.hidden = true; }, 2200);
  }

  function hide() {
    if (shown) clearInterval(shown.timer);
    shown = null;
  }

  async function reveal(p, v) {
    const r = await fetch("/v1/profiles/" + encodeURIComponent(p.id) + "/reveal", {
      method: "POST", headers: { "Content-Type": "application/json", "X-Helm-Reveal": "1" }, body: JSON.stringify({ var: v }) });
    if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
    return (await r.json()).value;
  }

  async function copyText(text, what) {
    try { await navigator.clipboard.writeText(text); toast(what + " copied"); }
    catch { toast("The browser blocked the clipboard. Use Show and copy it by hand.", true); }
  }

  const shq = (s) => "'" + String(s).replace(/'/g, "'\\''") + "'";

  // ---------- data ----------
  async function refresh() {
    try {
      await Promise.all([PF.load(true), kit.load()]);
      cost = await PF.cost(30, 0);
      failed = false;
    } catch { failed = true; }
    render();
  }

  const preset = (p) => PF.presetsFor(p.agent).find((x) => x.id === p.preset) || { fields: [] };
  const costOf = (p) => (cost && cost.profiles.find((c) => c.id === p.id)) || null;

  // ---------- render ----------
  function render() {
    const list = PF.profiles();
    $("pp-count").textContent = failed ? "Can't reach Helm. Start it with `hlm open`, then reload."
      : list.length + (list.length === 1 ? " profile" : " profiles") + " · keys are kept in secrets.json, readable only by you";
    if (!list.length) {
      hide();
      $("pp-page").replaceChildren(h("div", { class: "panel pp-empty" }, h("b", null, "No launch profiles yet"),
        "A profile starts agents on a company gateway, an API key or a cloud account instead of your own sign-in, with an optional budget. " +
        "Add one with + New profile, then pick it in + Agent on the Fleet page."));
      return;
    }
    if (!list.some((p) => p.id === sel)) sel = list[0].id;
    const p = list.find((x) => x.id === sel);
    if (shown && shown.pid !== p.id) hide();
    $("pp-page").replaceChildren(
      h("nav", { class: "pp-list", "aria-label": "profiles" }, list.map(item),
        h("div", { class: "pp-item pp-own" }, h("span", null, PF.dot(PF.NONE), " My login"), h("span", { class: "pp-m" }, "your own sign-in · no keys"))),
      detail(p));
  }

  function item(p) {
    const c = costOf(p), b = PF.budget(c);
    return h("button", { class: "pp-item", type: "button", "data-sel": p.id, "aria-current": String(p.id === sel) },
      h("span", { class: "one" }, PF.dot(p), " ", p.name),
      h("span", { class: "pp-m one" }, PF.kind(p)),
      h("span", { class: "pp-m" }, c ? PF.money(b.budget ? b.spent : c.range_usd) + (b.pct != null ? " · " + Math.round(b.pct) + "% of budget" : " · 30 days") : ""));
  }

  function detail(p) {
    const pr = preset(p), c = costOf(p), b = PF.budget(c);
    const rows = [];
    for (const f of pr.fields || []) rows.push(f.secret ? secretRow(p, f) : valueRow(f.label, f.var, (p.values || {})[f.var]));
    for (const [k, v] of Object.entries(p.extra || {})) rows.push(valueRow("Extra variable", k, v));
    const meter = b.pct != null ? h("div", { class: "prof-meter big pp-meter" }, h("i", { class: "t-" + PF.tone(b.pct), style: `width:${Math.min(100, b.pct).toFixed(1)}%` })) : null;
    rows.push(h("div", { class: "pp-field" }, h("div", null, "Budget", h("div", { class: "pp-var" }, p.budget_days ? p.budget_days + "-day window" : "since created",
      " · ", b.source === "gateway" ? "gateway's numbers" : "Helm's estimate")),
      h("div", { class: "pp-val" }, b.budget ? [meter, h("span", null, PF.money(b.spent) + " of " + PF.money(b.budget)), h("span", { class: "pp-m" }, Math.round(b.pct) + "%")]
        : h("span", { class: "pp-m" }, "no budget" + (c ? " · " + PF.money(c.range_usd) + " in 30 days" : "")))));
    if (c) rows.push(h("div", { class: "pp-field" }, h("div", null, "Use"), h("div", { class: "pp-val" }, c.sessions + (c.sessions === 1 ? " session" : " sessions") + " in 30 days · " + PF.money(c.range_usd))));
    const gw = (pr.fields || []).some((f) => f.url);
    const pb = probes[p.id];
    return h("section", { class: "panel pp-detail", "aria-label": p.name },
      h("div", { class: "pp-head" }, h("h2", { class: "pp-name" }, PF.dot(p), " ", p.name), h("div", { class: "pp-m" }, PF.kind(p) + " · " + kit.label(p.agent))),
      h("div", { class: "pp-fields" }, rows),
      h("div", { class: "pp-actions" },
        h("button", { class: "btn btn-secondary", type: "button", "data-act": "env" }, "Copy as env"),
        gw ? h("button", { class: "btn btn-secondary", type: "button", "data-act": "test" }, "Test connection") : null,
        h("button", { class: "btn btn-secondary", type: "button", "data-act": "edit" }, "Edit"),
        h("button", { class: "btn btn-secondary", type: "button", "data-act": "dup" }, "Duplicate")),
      pb ? h("p", { class: "pp-probe " + (pb.ok ? "ok" : "warn") }, pb.text) : null,
      h("p", { class: "pp-m" }, "Delete and the rest of the settings are under Edit. Agents already running keep the key they started with."));
  }

  function valueRow(label, v, val) {
    return h("div", { class: "pp-field" }, h("div", null, label, h("div", { class: "pp-var" }, v)),
      h("div", { class: "pp-val" }, val ? [h("code", null, val), h("button", { class: "btn btn-secondary pp-sm", type: "button", "data-copyval": val, "data-what": label }, "Copy")] : h("span", { class: "pp-m" }, "not set")));
  }

  function secretRow(p, f) {
    const v = f.var, label = f.label, set = (p.secrets || {})[v];
    const name = h("div", null, label, h("div", { class: "pp-var" }, v));
    if (replacing === v) {
      const input = h("input", { class: "pp-input", type: "password", id: "pp-new-key", autocomplete: "off", spellcheck: "false", placeholder: "paste the new key", "aria-label": "New " + label });
      return h("div", { class: "pp-field" }, name, h("div", { class: "pp-val" }, input,
        h("button", { class: "btn btn-primary pp-sm", type: "button", "data-save": v }, "Save"),
        h("button", { class: "btn btn-secondary pp-sm", type: "button", "data-cancel": "" }, "Cancel")));
    }
    const on = shown && shown.pid === p.id && shown.v === v;
    return h("div", { class: "pp-field" }, name, h("div", { class: "pp-val" },
      set ? h("code", { class: "pp-secret" + (on ? " shown" : "") }, on ? shown.value : (p.masks || {})[v] ? "••••••••" + p.masks[v].replace("…", "") : "••••••••") : h("span", { class: "pp-m" }, "not set"),
      on ? h("span", { class: "pp-count", id: "pp-left" }, "hides in " + shown.left + "s") : null,
      set ? h("button", { class: "btn btn-secondary pp-sm", type: "button", "data-show": v, "aria-pressed": String(on) }, on ? "Hide" : "Show") : null,
      set ? h("button", { class: "btn btn-secondary pp-sm", type: "button", "data-copy": v, "data-what": label }, "Copy") : null,
      h("button", { class: "btn btn-secondary pp-sm", type: "button", "data-replace": v }, set ? "Replace" : "Set")));
  }

  // ---------- actions ----------
  const current = () => PF.profiles().find((x) => x.id === sel);

  async function saveKey(p, v, value) {
    const body = { agent: p.agent, name: p.name, preset: p.preset, values: p.values || {}, extra: p.extra || {}, color: p.color || "",
      budget_usd: p.budget_usd || 0, budget_days: p.budget_days || 0, spend: p.spend || "", secrets: { [v]: value } };
    const r = await fetch("/v1/profiles/" + encodeURIComponent(p.id), { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
  }

  $("pp-page").addEventListener("click", async (e) => {
    const t = e.target.closest("button");
    if (!t) return;
    if (t.dataset.sel) { sel = t.dataset.sel; replacing = ""; hide(); history.replaceState(null, "", "#" + encodeURIComponent(sel)); return render(); }
    const p = current();
    if (!p) return;
    try {
      if (t.dataset.copyval != null) return copyText(t.dataset.copyval, t.dataset.what);
      if (t.dataset.show) {
        const v = t.dataset.show;
        if (shown && shown.v === v) { hide(); return render(); }
        hide();
        const value = await reveal(p, v);
        shown = { pid: p.id, v, value, left: SHOW_SECONDS };
        shown.timer = setInterval(() => {
          if (!shown) return;
          if (--shown.left <= 0) { hide(); render(); return; }
          const c = $("pp-left");
          if (c) c.textContent = "hides in " + shown.left + "s";
        }, 1000);
        return render();
      }
      if (t.dataset.copy) return copyText(await reveal(p, t.dataset.copy), t.dataset.what);
      if (t.dataset.replace) { replacing = t.dataset.replace; hide(); render(); $("pp-new-key").focus(); return; }
      if (t.dataset.cancel != null) { replacing = ""; return render(); }
      if (t.dataset.save) {
        const value = $("pp-new-key").value.trim();
        if (!value) return $("pp-new-key").focus();
        await saveKey(p, t.dataset.save, value);
        replacing = "";
        toast("Key replaced for " + p.name);
        return refresh();
      }
      const act = t.dataset.act;
      if (act === "env") {
        const pr = preset(p), lines = [];
        for (const f of pr.fields || []) {
          const v = f.var;
          if (f.secret) { if ((p.secrets || {})[v]) lines.push("export " + v + "=" + shq(await reveal(p, v))); }
          else if ((p.values || {})[v]) lines.push("export " + v + "=" + shq(p.values[v]));
        }
        for (const [k, v] of Object.entries(p.extra || {})) lines.push("export " + k + "=" + shq(v));
        return copyText(lines.join("\n") + "\n", "Environment for " + p.name);
      }
      if (act === "test") {
        t.disabled = true;
        t.textContent = "Testing…";
        const r = await fetch("/v1/profiles/probe", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id: p.id }) });
        const res = r.ok ? await r.json() : { error: (await r.text()).trim() };
        probes[p.id] = res.litellm
          ? { ok: true, text: "✓ The gateway answered: this key has spent " + PF.money(res.spend.spend_usd) + (res.spend.budget_usd != null ? " of " + PF.money(res.spend.budget_usd) : ", no limit on the key") + "." }
          : { ok: false, text: (res.error || "Not a LiteLLM gateway") + "." };
        return render();
      }
      if (act === "edit") return PF.edit(p, { onSaved: (np) => { if (np === null) sel = ""; refresh(); } });
      if (act === "dup") return PF.edit(p, { duplicate: true, onSaved: (np) => { if (np) sel = np.id; refresh(); } });
    } catch (x) {
      toast(String(x.message || x), true);
    }
  });
  $("pp-page").addEventListener("keydown", (e) => {
    if (e.key === "Enter" && e.target.id === "pp-new-key") { e.preventDefault(); $("pp-page").querySelector("[data-save]").click(); }
    if (e.key === "Escape" && e.target.id === "pp-new-key") { replacing = ""; render(); }
  });
  $("pp-new").addEventListener("click", () => PF.edit(null, { onSaved: (np) => { if (np) sel = np.id; refresh(); } }));
  addEventListener("hashchange", () => { sel = decodeURIComponent(location.hash.slice(1)); hide(); render(); });
  // A key on screen goes away when you leave the tab.
  document.addEventListener("visibilitychange", () => { if (document.hidden && shown) { hide(); render(); } });

  $("host").textContent = location.host;
  refresh();
  setInterval(() => { if (!document.hidden && !replacing && !shown) refresh(); }, 60e3);
})();
