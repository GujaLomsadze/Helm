"use strict";
// Usage page, API keys section: every launch profile's spend against its budget, and a chart of
// spend climbing over the chosen range with each budget as a ceiling. Spend is Claude Code's own
// cost estimate (GET /v1/cost); a profile on a LiteLLM gateway also shows the gateway's count.
// Uses h/svgEl/$/now/dur from limits.js and HelmProfiles from profiles.js.

(function () {
  const PF = window.HelmProfiles, box = $("api-keys");
  if (!box || !PF) return;
  const RANGE_KEY = "hlm.usage.days";
  let days = (() => { try { return Number(localStorage.getItem(RANGE_KEY)) || 30; } catch { return 30; } })();
  if (![7, 30, 90].includes(days)) days = 30;
  let data = null, failed = false, hover = false;
  const shortDay = (t) => new Date(t).toLocaleDateString("en", { month: "short", day: "numeric" });

  async function refresh(maxAge = 0) {
    try {
      await PF.load();
      data = await PF.cost(days, maxAge);
      failed = false;
    } catch { failed = true; }
    if (!hover) render();
  }

  function render() {
    const seg = h("div", { class: "seg", role: "group", "aria-label": "range" },
      [7, 30, 90].map((d) => h("button", { type: "button", "aria-pressed": String(d === days), "data-days": d }, d + " days")));
    const add = h("button", { class: "btn btn-secondary", type: "button", "data-add": "" }, "+ Add profile");
    const head = h("div", { class: "chart-head" }, h("h2", null, "API keys"), h("div", { class: "keys-tools" }, seg, add));
    if (!data) {
      box.replaceChildren(head, h("div", { class: "empty-note" }, h("b", null, failed ? "Can't reach Helm" : "Loading"),
        failed ? "The daemon didn't answer. Start it with `hlm open`, then reload." : ""));
      return;
    }
    const rows = data.profiles.map((cp) => {
      const p = cp.id ? PF.byId(cp.id) || { ...cp } : PF.NONE;
      return { cp, p, b: PF.budget(cp) };
    });
    const real = rows.filter((r) => r.cp.id);
    if (!PF.profiles().length && !real.length) {
      box.replaceChildren(head, h("div", { class: "empty-note" }, h("b", null, "No launch profiles yet"),
        "A profile starts agents on a company gateway, an API key or a cloud account instead of your own sign-in, " +
        "with an optional budget. Add one, then pick it in + Agent. Spend shows here."),
        rows.length ? keyList(rows) : null);
      return;
    }
    const legend = h("div", { class: "legend" },
      h("span", null, h("i", { class: "k" }), "spent, Helm's estimate"),
      h("span", null, h("i", { class: "k dash" }), "next 7 days at the last 7 days' pace"),
      h("span", null, h("i", { class: "k capk" }), "budget"));
    const plot = h("div", { class: "plot keys-plot" });
    box.replaceChildren(head, h("div", { class: "keys-body" }, keyList(rows), h("div", { class: "keys-chart" }, plot, legend)),
      h("p", { class: "meta" }, "My login is what your own sessions would cost at API prices; your plan covers it. " +
        "Gateway figures refresh once a minute while this page is open."));
    draw(plot, rows);
  }

  function keyList(rows) {
    return h("div", { class: "keys" }, rows.map(({ cp, p, b }) => {
      const t = PF.tone(b.pct);
      let src;
      if (!cp.id) src = "at API prices · covered by your plan";
      else if (b.source === "gateway") src = `gateway: ${PF.money(b.spent)}${b.budget ? " of " + PF.money(b.budget) : ", no limit on the key"}` +
        (b.resetAt ? " · resets " + shortDay(b.resetAt) : "") + ` · Helm's estimate ${PF.money(cp.window_usd)}`;
      else if (cp.gateway_error) src = "gateway: " + cp.gateway_error + " · showing Helm's estimate";
      else if (b.budget) src = "Helm's estimate · " + (cp.budget_days ? "last " + cp.budget_days + (cp.budget_days === 1 ? " day" : " days") : "since created");
      else src = "no budget · Helm's estimate";
      const big = b.budget ? b.spent : cp.range_usd;
      return h("div", { class: "key" + (cp.deleted ? " deleted" : "") },
        h("div", { class: "key-name" }, h("div", { class: "one" }, PF.dot(p), " ", cp.deleted ? "Deleted profile" : p.name),
          h("div", { class: "src one" }, cp.deleted ? "spend of sessions that ran on it" : PF.kind(p) + " · " + cp.sessions + (cp.sessions === 1 ? " session" : " sessions") + " in " + days + " days")),
        h("div", { class: "key-m" },
          b.pct != null ? h("div", { class: "prof-meter big" }, h("i", { class: "t-" + t, style: `width:${Math.min(100, b.pct).toFixed(1)}%` })) : null,
          h("div", { class: "src" }, src)),
        h("div", { class: "key-v" + (t === "red" ? " c-red" : t === "yellow" ? " c-yellow" : "") },
          PF.money(big), b.budget ? h("small", null, " / " + PF.money(b.budget)) : h("small", null, " · " + days + "d")),
        cp.id && !cp.deleted ? h("button", { class: "btn btn-secondary key-edit", type: "button", "data-edit": cp.id, "aria-label": "Edit " + p.name, title: "Edit profile" }, "✎") : h("span"));
    }));
  }

  // Cumulative spend per profile over the range; each budget whose window matches the range is a ceiling.
  function draw(plot, rows) {
    const W = plot.clientWidth || 600, H = plot.clientHeight || 300;
    const narrow = W < 520, ml = 52, mr = narrow ? 14 : 130, mt = 16, mb = 26;
    const t0 = data.since, tNow = data.now, t1 = tNow + 7 * DAY * (days <= 7 ? 0.5 : 1);
    const by = {};
    for (const pt of data.series) (by[pt.profile] = by[pt.profile] || []).push(pt);
    const lines = rows.map(({ cp, p, b }) => {
      const pts = (by[cp.id] || []).sort((a, z) => a.hour - z.hour);
      let acc = 0;
      const xs = [[t0, 0]];
      for (const q of pts) { acc += q.usd; xs.push([q.hour + HOUR / 2, acc]); }
      xs.push([tNow, acc]);
      const recent = pts.filter((q) => q.hour >= tNow - 7 * DAY).reduce((a, q) => a + q.usd, 0);
      const span = Math.min(7 * DAY, tNow - t0);
      const proj = acc + (span > 0 ? (recent / span) * (t1 - tNow) : 0);
      const ceiling = b.budget && cp.id && (cp.budget_days === days || (!cp.budget_days && b.source !== "gateway")) ? b.budget : 0;
      return { cp, p, xs, acc, proj, ceiling, color: PF.color(p) };
    }).filter((l) => l.acc > 0 || l.ceiling);
    if (!lines.length) {
      plot.replaceChildren(h("div", { class: "empty-note" }, h("b", null, "No spend yet"), "Nothing spent in the last " + days + " days."));
      return;
    }
    let max = 0;
    for (const l of lines) max = Math.max(max, l.proj, l.ceiling);
    max = max * 1.1 || 1;
    const x = (t) => ml + ((t - t0) / (t1 - t0)) * (W - ml - mr);
    const y = (v) => H - mb - (v / max) * (H - mt - mb);
    const svg = svgEl("svg", { viewBox: `0 0 ${W} ${H}`, role: "img", "aria-label": "spend per profile over the last " + days + " days" });
    const step = niceStep(max / 4);
    for (let v = 0; v <= max; v += step) {
      svgEl("line", { class: "grid-l", x1: ml, x2: W - mr, y1: y(v), y2: y(v) }, svg);
      svgEl("text", { class: "ax", x: ml - 8, y: y(v) + 4, "text-anchor": "end" }, svg, "$" + (step < 1 ? v.toFixed(2) : Math.round(v)));
    }
    // date ticks at least ~90px apart, whatever the width
    const pxPerDay = (W - ml - mr) / ((t1 - t0) / DAY);
    const tick = [1, 2, 7, 14, 30, 60].find((d) => d * pxPerDay >= 90) * DAY || 90 * DAY;
    for (let t = t0; t <= t1 - tick / 3; t += tick) svgEl("text", { class: "ax", x: x(t), y: H - 6, "text-anchor": t === t0 ? "start" : "middle" }, svg, shortDay(t));
    svgEl("line", { class: "nowl", x1: x(tNow), x2: x(tNow), y1: mt, y2: H - mb }, svg);
    svgEl("text", { class: "lbl m", x: x(tNow), y: mt - 3, "text-anchor": "middle" }, svg, "now");
    const labels = [];
    for (const l of lines) {
      if (l.ceiling) svgEl("line", { x1: ml, x2: W - mr, y1: y(l.ceiling), y2: y(l.ceiling), stroke: l.color, "stroke-dasharray": "2 4", opacity: "0.8" }, svg);
      svgEl("polyline", { fill: "none", stroke: l.color, "stroke-width": l.cp.id ? 2.5 : 1.5, "stroke-linejoin": "round", opacity: l.cp.id ? 1 : 0.7,
        points: l.xs.map(([t, v]) => x(t).toFixed(1) + "," + y(v).toFixed(1)).join(" ") }, svg);
      svgEl("line", { x1: x(tNow), y1: y(l.acc), x2: x(t1), y2: y(l.proj), stroke: l.color, "stroke-width": 2, "stroke-dasharray": "6 5", opacity: "0.6" }, svg);
      labels.push({ y: y(l.proj), text: narrow ? "" : (l.cp.id ? l.p.name : "My login"), color: l.color });
    }
    // labels at the right end, pushed apart so they never overlap
    labels.sort((a, z) => a.y - z.y);
    for (let i = 1; i < labels.length; i++) labels[i].y = Math.max(labels[i].y, labels[i - 1].y + 15);
    for (const lb of labels) if (lb.text) svgEl("text", { class: "lbl", x: x(t1) + 6, y: lb.y + 4, fill: lb.color, style: "fill:" + lb.color }, svg, lb.text);
    const xh = svgEl("line", { class: "xh", y1: mt, y2: H - mb, visibility: "hidden" }, svg);
    const tip = h("div", { class: "tip", hidden: "" });
    plot.replaceChildren(svg, tip);
    plot.onpointermove = (e) => {
      const r = svg.getBoundingClientRect(), px = ((e.clientX - r.left) / r.width) * W;
      const t = t0 + ((px - ml) / (W - ml - mr)) * (t1 - t0);
      if (t < t0 || t > tNow) { xh.setAttribute("visibility", "hidden"); tip.hidden = true; return; }
      hover = true;
      xh.setAttribute("x1", x(t)); xh.setAttribute("x2", x(t)); xh.setAttribute("visibility", "visible");
      tip.replaceChildren(h("div", { class: "m" }, shortDay(t)), ...lines.map((l) => {
        let v = 0;
        for (const [tt, vv] of l.xs) if (tt <= t) v = vv;
        return h("div", null, PF.dot(l.p), " ", h("b", null, PF.money(v)), h("span", { class: "m" }, l.cp.id ? l.p.name : "My login"));
      }));
      tip.hidden = false;
      tip.style.left = ((x(t) / W) * 100).toFixed(1) + "%";
      tip.style.top = mt + 10 + "px";
    };
    plot.onpointerleave = () => { hover = false; xh.setAttribute("visibility", "hidden"); tip.hidden = true; };
  }
  function niceStep(raw) {
    const p = Math.pow(10, Math.floor(Math.log10(raw || 1))), n = raw / p;
    return (n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10) * p;
  }

  box.addEventListener("click", (e) => {
    const d = e.target.closest("[data-days]");
    if (d) {
      days = Number(d.dataset.days);
      try { localStorage.setItem(RANGE_KEY, String(days)); } catch { /* per-browser convenience */ }
      return refresh();
    }
    if (e.target.closest("[data-add]")) return PF.edit(null, { onSaved: () => refresh() });
    const ed = e.target.closest("[data-edit]");
    if (ed) { const p = PF.byId(ed.dataset.edit); if (p) PF.edit(p, { onSaved: () => refresh() }); }
  });
  refresh();
  setInterval(() => { if (!document.hidden) refresh(); }, 60e3);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) refresh(); });
  let rz;
  addEventListener("resize", () => { clearTimeout(rz); rz = setTimeout(() => { if (data) render(); }, 100); });
})();
