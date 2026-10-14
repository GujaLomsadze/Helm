"use strict";
(() => {
  const $ = (id) => document.getElementById(id);
  const node = (tag, text, className) => { const e = document.createElement(tag); if (text != null) e.textContent = text; if (className) e.className = className; return e; };
  const stamp = (ms) => ms ? new Date(ms).toLocaleString() : "—";
  const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  let jobs = [], enabled = false, selectedProvider = "claude", editing = null, historyID = null, refreshing = false, submitting = false;
  let jobsSignature = "", historySignature = "";
  $("host").textContent = location.host;

  async function request(path, method = "GET", body) {
    const response = await fetch("/v1/schedules" + path, { method, headers: body === undefined ? {} : { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body) });
    if (!response.ok) throw new Error((await response.text()).trim() || response.statusText);
    return response.status === 204 ? null : response.json();
  }

  function button(label, action, disabled = false) {
    const b = node("button", label, "btn btn-secondary"); b.type = "button"; b.disabled = disabled;
    b.addEventListener("click", async () => {
      b.disabled = true;
      try { await action(); $("page-error").textContent = ""; await refresh(); }
      catch (error) { $("page-error").textContent = error.message; }
      finally { if (b.isConnected) b.disabled = disabled; }
    });
    return b;
  }

  function cadence(j) {
    if (j.repeat === "once") return "One time";
    if (j.repeat === "interval") return `Every ${j.every_minutes} min`;
    const when = new Intl.DateTimeFormat(undefined, { timeZone: j.timezone, hour: "2-digit", minute: "2-digit", ...(j.repeat === "weekly" ? { weekday: "long" } : {}) }).format(j.run_at);
    return `${j.repeat === "daily" ? "Daily" : "Weekly"} · ${when} · ${j.timezone}`;
  }

  function drawJobs() {
    const list = $("schedules");
    if (!jobs.length) { list.replaceChildren(node("div", "No scheduled tasks yet. Schedule your first task to get started.", "panel sc-empty")); return; }
    list.replaceChildren(...jobs.map((j) => {
      const card = node("article", null, "panel sc-card"), head = node("div", null, "sc-card-head"), title = node("h3");
      const logo = node("img"); logo.src = `icons/${j.provider}.svg`; logo.alt = j.provider === "codex" ? "Codex" : "Claude Code";
      title.append(logo, document.createTextNode(j.name));
      const state = j.running ? "running" : !j.enabled ? "paused" : !j.next_run_at ? "completed" : "scheduled";
      head.append(title, node("span", state, "sc-status " + state));
      const details = node("div", null, "sc-details");
      details.append(node("span", cadence(j)), node("span", `Next: ${j.enabled ? (j.next_run_at ? stamp(j.next_run_at) : "complete") : "paused"}`), node("span", `Time limit: ${j.timeout_minutes} min`));
      const actions = node("div", null, "sc-actions");
      actions.append(
        button("Run now", () => request(`/${j.id}/run`, "POST"), j.running || !enabled),
        button(j.enabled ? "Pause" : "Resume", () => request(`/${j.id}/enabled`, "POST", { enabled: !j.enabled })),
        button("Edit", () => openForm(j), j.running),
        button("History", async () => { historyID = j.id; historySignature = ""; $("history-title").textContent = j.name + " · Run history"; $("history").replaceChildren(node("p", "Loading…", "meta")); $("history-dialog").showModal(); await loadHistory(); }),
        button("Delete", async () => { if (confirm(`Delete “${j.name}” and its run history?`)) await request(`/${j.id}`, "DELETE"); }, j.running)
      );
      if (j.running) actions.append(button("Stop run", () => request(`/${j.id}/stop`, "POST")));
      card.append(head, details, node("p", j.prompt, "sc-prompt"));
      if (j.last_run) card.append(node("span", `Last run: ${j.last_run.status} · ${stamp(j.last_run.started_at)}${j.last_run.error ? " · " + j.last_run.error : ""}`, "sc-status " + j.last_run.status));
      card.append(actions); return card;
    }));
  }

  async function loadHistory() {
    if (!historyID || !$("history-dialog").open) return;
    const id = historyID, data = await request(`/${id}/runs`);
    if (historyID !== id || !$("history-dialog").open) return;
    const signature = JSON.stringify(data.runs);
    if (signature === historySignature) return;
    historySignature = signature;
    const old = new Map(Array.from($("history").querySelectorAll("details")).map((d) => [d.dataset.run, { open: d.open, scroll: d.querySelector("pre").scrollTop }]));
    if (!data.runs.length) { $("history").replaceChildren(node("p", "This task has not run yet.", "meta")); return; }
    $("history").replaceChildren(...data.runs.map((r, index) => {
      const detail = node("details", null, "sc-run"); detail.dataset.run = r.id; detail.open = old.has(r.id) ? old.get(r.id).open : index === 0;
      const summary = node("summary"); summary.append(node("span", r.status, "sc-status " + r.status), node("span", stamp(r.started_at)), node("span", r.finished_at ? `${Math.max(0, Math.round((r.finished_at - r.started_at) / 1000))}s · exit ${r.exit_code ?? "—"}` : "In progress"));
      detail.append(summary);
      if (r.error) detail.append(node("p", r.error, "ag-err"));
      if (r.status === "running" && r.terminal_id) { const link = node("a", "Open live terminal ↗"); link.href = `term.html?id=${encodeURIComponent(r.terminal_id)}`; detail.append(link); }
      const output = node("pre", (r.output || "No output recorded yet.").replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "").replace(/\x1b\][^\x07]*(?:\x07|\x1b\\)/g, "").replace(/\r\n/g, "\n"));
      detail.append(output);
      if (old.has(r.id)) requestAnimationFrame(() => { output.scrollTop = old.get(r.id).scroll; });
      return detail;
    }));
  }

  async function refresh() {
    if (refreshing) return;
    refreshing = true;
    try {
      const data = await request(""); jobs = data.schedules; enabled = data.enabled && data.supported;
      $("state").textContent = "● live";
      $("availability").hidden = enabled;
      $("availability").textContent = data.supported ? "Scheduled tasks are waiting: enable Terminals in Fleet → ⚙ Card layout. Your schedules are saved." : "Running agents is unavailable on this platform. Your schedules are saved.";
      // Leave focus, hover, and text selection intact when polling unchanged jobs.
      const signature = JSON.stringify([jobs, enabled]);
      if (signature !== jobsSignature) { jobsSignature = signature; drawJobs(); }
      await loadHistory();
    } catch (error) { $("state").textContent = "○ disconnected"; $("page-error").textContent = error.message; }
    finally { refreshing = false; }
  }

  function localInput(ms) {
    const d = new Date(ms); return new Date(ms - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  }
  function provider(value) {
    selectedProvider = value;
    document.querySelectorAll("[data-provider]").forEach((b) => b.setAttribute("aria-pressed", String(b.dataset.provider === value)));
  }
  function repeatChanged() { $("interval-field").hidden = $("task-repeat").value !== "interval"; }
  async function openForm(job = null) {
    editing = job;
    $("task-form").reset(); $("form-error").textContent = ""; provider(job?.provider || "claude");
    $("task-title").textContent = job ? "Edit scheduled task" : "Schedule task";
    $("task-name").value = job?.name || ""; $("task-dir").value = job?.dir || ""; $("task-prompt").value = job?.prompt || "";
    $("task-repeat").value = job?.repeat || "once";
    $("task-at").value = localInput(job?.run_at || Math.ceil((Date.now() + 5 * 60000) / 60000) * 60000);
    $("task-every").value = job?.every_minutes || 60; $("task-timeout").value = job?.timeout_minutes || 60;
    $("timezone-note").textContent = `First run is shown in ${browserZone}. Daily and weekly repeats follow ${job?.timezone || browserZone}, including daylight saving time.`;
    repeatChanged(); $("task-dialog").showModal(); $("task-name").focus();
    try {
      const data = await (await fetch("/v1/sessions")).json();
      $("recent-dirs").replaceChildren(...[...new Set((data.sessions || []).map((s) => s.cwd).filter(Boolean))].map((dir) => { const option = node("option"); option.value = dir; return option; }));
    } catch { /* Folder entry remains available without recent-session suggestions. */ }
  }

  $("task-form").addEventListener("submit", async (event) => {
    event.preventDefault(); if (submitting) return;
    submitting = true; $("save-task").disabled = true;
    try {
      const unchangedAt = editing && $("task-at").value === localInput(editing.run_at);
      const config = { name: $("task-name").value, provider: selectedProvider, dir: $("task-dir").value.trim(), prompt: $("task-prompt").value, repeat: $("task-repeat").value, run_at: unchangedAt ? editing.run_at : new Date($("task-at").value).getTime(), every_minutes: Number($("task-every").value), timezone: editing?.timezone || browserZone, timeout_minutes: Number($("task-timeout").value) };
      await request(editing ? `/${editing.id}` : "", editing ? "PUT" : "POST", config);
      $("task-dialog").close(); await refresh();
    } catch (error) { $("form-error").textContent = error.message; }
    finally { submitting = false; $("save-task").disabled = false; }
  });
  $("new-task").addEventListener("click", () => void openForm());
  for (const id of ["close-task", "cancel-task"]) $(id).addEventListener("click", () => $("task-dialog").close());
  $("close-history").addEventListener("click", () => $("history-dialog").close());
  $("history-dialog").addEventListener("close", () => { historyID = null; });
  $("task-repeat").addEventListener("change", repeatChanged);
  document.querySelectorAll("[data-provider]").forEach((b) => b.addEventListener("click", () => provider(b.dataset.provider)));
  void refresh(); setInterval(() => { if (!document.hidden) void refresh(); }, 3000);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) void refresh(); });
})();
