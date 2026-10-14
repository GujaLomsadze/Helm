"use strict";
const http = require("node:http");
const { spawn } = require("node:child_process");
const { setTimeout: delay } = require("node:timers/promises");

function originForPort(value) {
  const port = value == null || value === "" ? 7777 : Number(value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error("TRK_PORT must be a port between 1 and 65535.");
  return `http://127.0.0.1:${port}`;
}

function dashboardURL(value, origin) {
  try {
    const u = new URL(value);
    return u.origin === origin && !u.username && !u.password && ["/", "/index.html", "/limits.html", "/stats.html", "/term.html"].includes(u.pathname);
  } catch { return false; }
}

function health(origin) {
  return new Promise((resolve, reject) => {
    const req = http.get(origin + "/healthz", { timeout: 1000 }, (res) => {
      let body = "";
      res.setEncoding("utf8");
      res.on("data", (chunk) => {
        body += chunk;
        if (body.length > 8192) req.destroy(new Error("Unexpected service on the TRK port."));
      });
      res.on("end", () => {
        try {
          const value = JSON.parse(body);
          if (res.statusCode !== 200 || value.app !== "trk" || value.ok !== true) throw new Error();
          resolve(value);
        } catch { reject(new Error("Another service is using the TRK port. Close it or choose a different TRK_PORT.")); }
      });
      res.on("error", reject);
    });
    req.on("timeout", () => req.destroy(new Error("TRK is not responding on its local port.")));
    req.on("error", (error) => error.code === "ECONNREFUSED" ? resolve(null) : reject(error));
  });
}

async function ensureBackend({ origin, executable, env = process.env, launch = spawn, probe = health, timeout = 15000 }) {
  if (await probe(origin)) return { reused: true };
  // A detached daemon keeps hooks and owned agents alive after the window closes.
  // Never stop, replace, or upgrade a daemon that another TRK client owns.
  const childEnv = { ...env, TRK_PORT: new URL(origin).port, TRK_URL: origin };
  for (const key of ["ELECTRON_RUN_AS_NODE", "TRK_SESSION", "TRK_TERMINAL_ID", "CODEX_THREAD_ID", "CODEX_SESSION_ID", "CODEX_APP_TOOLS_PIPE_PATH", "CODEX_INTERNAL_ORIGINATOR_OVERRIDE"]) delete childEnv[key];
  const child = launch(executable, ["serve"], { detached: true, windowsHide: true, stdio: "ignore", env: childEnv });
  let failure;
  child.on("error", () => { failure = new Error("Could not start the bundled TRK backend. Reinstall TRK or check that its files are present."); });
  child.on("exit", (code) => { if (code) failure = new Error(`TRK backend exited with code ${code}. See the tracker log in your TRK data directory.`); });
  child.unref();
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (failure) throw failure;
    if (await probe(origin)) return { reused: false };
    await delay(150);
  }
  throw new Error("TRK did not become ready. Check trk.log in your TRK data directory, then open the app again.");
}

module.exports = { originForPort, dashboardURL, health, ensureBackend };
