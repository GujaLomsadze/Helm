"use strict";
const { app, BrowserWindow, Menu, dialog, session } = require("electron");
const path = require("node:path");
const fs = require("node:fs");
const { pathToFileURL } = require("node:url");
const { originForPort, dashboardURL, ensureBackend } = require("./backend.cjs");

const smoke = process.argv.includes("--smoke-test");
const loadingPath = path.join(__dirname, "loading.html");
const loadingURL = pathToFileURL(loadingPath).href;
let mainWindow;
let origin;
const preferences = { nodeIntegration: false, contextIsolation: true, sandbox: true, webSecurity: true, webviewTag: false };

function secureWindow(win) {
  const contents = win.webContents;
  contents.on("will-navigate", (event, url) => {
    if (url !== loadingURL && !dashboardURL(url, origin)) event.preventDefault();
  });
  contents.on("will-redirect", (event, url) => {
    if (!dashboardURL(url, origin)) event.preventDefault();
  });
  contents.setWindowOpenHandler(({ url }) => {
    if (!dashboardURL(url, origin) || new URL(url).pathname !== "/term.html") return { action: "deny" };
    return { action: "allow", overrideBrowserWindowOptions: { width: 1100, height: 720, backgroundColor: "#0B0605", autoHideMenuBar: true, webPreferences: preferences } };
  });
  contents.on("did-create-window", secureWindow);
  contents.on("will-attach-webview", (event) => event.preventDefault());
}

async function openWindow() {
  if (mainWindow && !mainWindow.isDestroyed()) {
    if (mainWindow.isMinimized()) mainWindow.restore();
    mainWindow.show(); mainWindow.focus(); return;
  }
  const win = new BrowserWindow({ title: "TRK", width: 1440, height: 920, minWidth: 780, minHeight: 560,
    show: false, backgroundColor: "#0B0605", autoHideMenuBar: true,
    icon: path.join(__dirname, "assets", "trk.png"), webPreferences: preferences });
  mainWindow = win;
  secureWindow(win);
  win.on("closed", () => { if (mainWindow === win) mainWindow = null; });
  win.once("ready-to-show", () => { if (!smoke && !win.isDestroyed()) win.show(); });
  await win.loadFile(loadingPath);
  const executable = path.join(app.isPackaged ? process.resourcesPath : __dirname, app.isPackaged ? "backend" : "resources", process.platform === "win32" ? "trk.exe" : "trk");
  try {
    const state = await ensureBackend({ origin, executable });
    if (win.isDestroyed()) return;
    await win.loadURL(origin + "/");
    if (smoke) {
      fs.writeFileSync(path.join(app.getPath("userData"), "smoke-result.json"), JSON.stringify({ ok: true, packaged: app.isPackaged, url: win.webContents.getURL(), title: win.webContents.getTitle(), reusedBackend: state.reused, preferences }, null, 2));
      app.quit();
    }
  } catch (error) {
    if (smoke) { console.error(error.message); app.exit(1); return; }
    if (!win.isDestroyed()) {
      await dialog.showMessageBox(win, { type: "error", title: "TRK could not start", message: error.message, buttons: ["Close"] });
      win.close();
    }
  }
}

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", () => { if (app.isReady()) void openWindow(); });
  app.whenReady().then(async () => {
    try { origin = originForPort(process.env.TRK_PORT); }
    catch (error) { dialog.showErrorBox("TRK", error.message); app.quit(); return; }
    app.setAppUserModelId("com.trk.desktop");
    // The dashboard needs no Node bridge or general device permissions.
    const canWriteClipboard = (contents, permission) => permission === "clipboard-sanitized-write" && contents && dashboardURL(contents.getURL(), origin);
    session.defaultSession.setPermissionRequestHandler((contents, permission, callback) => callback(!!canWriteClipboard(contents, permission)));
    session.defaultSession.setPermissionCheckHandler((contents, permission) => !!canWriteClipboard(contents, permission));
    session.defaultSession.on("will-download", (event) => event.preventDefault());
    Menu.setApplicationMenu(Menu.buildFromTemplate([
      { label: "TRK", submenu: [{ label: "Dashboard", click: () => void openWindow() }, { type: "separator" }, { role: "quit" }] },
      { role: "editMenu" },
      { label: "View", submenu: [{ role: "reload" }, { role: "resetZoom" }, { role: "zoomIn" }, { role: "zoomOut" }, { role: "togglefullscreen" }] }
    ]));
    await openWindow();
    app.on("activate", () => { if (!BrowserWindow.getAllWindows().length) void openWindow(); });
  }).catch((error) => { console.error(error); app.exit(1); });
  // Closing a window never stops a shared backend or kills a running agent.
  app.on("window-all-closed", () => { if (process.platform !== "darwin" || smoke) app.quit(); });
}
