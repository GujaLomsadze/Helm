"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");
const http = require("node:http");
const { originForPort, dashboardURL, health, ensureBackend } = require("../backend.cjs");

test("only local tracker pages can navigate inside the app", () => {
  const origin = originForPort();
  assert.equal(origin, "http://127.0.0.1:7777");
  for (const value of ["0", "65536", "hello", "4.2"]) assert.throws(() => originForPort(value));
  assert.equal(dashboardURL(origin + "/term.html?id=abc", origin), true);
  for (const value of ["https://example.com", "file:///C:/secret", "javascript:alert(1)", "http://127.0.0.1:7778/", origin + "/v1/shutdown", "http://user@127.0.0.1:7777/"]) assert.equal(dashboardURL(value, origin), false);
});

test("existing backend is reused without spawning or stopping agents", async () => {
  const state = await ensureBackend({ origin:originForPort(), executable:"unused", probe:async()=>({ok:true}), launch:()=>assert.fail("must reuse") });
  assert.equal(state.reused,true);
});

test("cold start spawns a hidden detached backend with clean identity", async () => {
  let probes=0;
  const state=await ensureBackend({ origin:originForPort(), executable:"C:\\TRK\\trk.exe", env:{CODEX_THREAD_ID:"parent",TRK_SESSION:"parent",CODEX_HOME:"config"},
    probe:async()=>++probes>1?{ok:true}:null,
    launch:(exe,args,options)=>{
      assert.equal(exe,"C:\\TRK\\trk.exe"); assert.deepEqual(args,["serve"]);
      assert.equal(options.windowsHide,true);assert.equal(options.detached,true);assert.equal(options.stdio,"ignore");
      assert.equal(options.env.CODEX_THREAD_ID,undefined);assert.equal(options.env.TRK_SESSION,undefined);assert.equal(options.env.CODEX_HOME,"config");
      const child=new EventEmitter();child.unref=()=>{};return child;
    } });
  assert.equal(state.reused,false);
});

test("unrelated service cannot impersonate the tracker", async(t)=>{
  const server=http.createServer((_req,res)=>res.end('{"ok":true,"app":"other"}'));
  await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));
  t.after(()=>server.close());
  await assert.rejects(health(`http://127.0.0.1:${server.address().port}`),/Another service/);
});

test("missing bundled executable reports startup failure", async()=>{
  await assert.rejects(ensureBackend({origin:originForPort(),executable:"missing",probe:async()=>null,launch:()=>{
    const child=new EventEmitter();child.unref=()=>{};queueMicrotask(()=>child.emit("error",new Error("missing")));return child;
  }}),/bundled TRK backend/);
});
