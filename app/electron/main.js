import { app, BrowserWindow, ipcMain, shell } from "electron";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

let mainWindow = null;
let captureProcess = null;

function emitCaptureLog(level, message) {
  const payload = {
    level,
    message,
    timestamp: Date.now(),
  };
  console[level === "error" ? "error" : "log"](`[capture:${level}] ${message}`);
  mainWindow?.webContents.send("capture:log", payload);
}

function stopCaptureClient() {
  if (captureProcess) {
    emitCaptureLog("info", "Stopping capture client");
    captureProcess.kill();
    captureProcess = null;
  }
}

function startCaptureClient(args) {
  if (!args?.capture_session_id) {
    throw new Error("Missing capture_session_id");
  }

  stopCaptureClient();
  emitCaptureLog("info", "Capture triggered natively via React frontend.");
  return { ok: true };
}

function createMainWindow() {
  mainWindow = new BrowserWindow({
    width: 320,
    height: 520,
    minWidth: 320,
    minHeight: 520,
    frame: false,
    transparent: true,
    alwaysOnTop: true,
    resizable: true,
    minimizable: true,
    closable: true,
    skipTaskbar: false,
    title: "NoVo Transcriber",
    titleBarStyle: "hiddenInset",
    webPreferences: {
      preload: join(__dirname, "preload.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  // commenting for video recording in zoom/loom/etc. as screenshot capture of Electron app does not work with content protection enabled
  // uncomment this this while using the app for making notion notes
  // try {
  //   mainWindow.setContentProtection(true);
  // } catch (error) {
  //   console.warn("[novo-transcriber] content protection unavailable:", error);
  // }

  mainWindow.loadURL("http://localhost:5173");
}

app.whenReady().then(() => {
  createMainWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createMainWindow();
    }
  });
});

app.on("window-all-closed", () => {
  stopCaptureClient();
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("before-quit", () => {
  stopCaptureClient();
});

ipcMain.handle("window:minimize", () => {
  mainWindow?.minimize();
  return { ok: true };
});

ipcMain.handle("window:close", () => {
  mainWindow?.close();
  return { ok: true };
});

ipcMain.handle("shell:openExternal", async (_event, url) => {
  await shell.openExternal(url);
  return { ok: true };
});

ipcMain.handle("capture:start", (_event, args) => startCaptureClient(args));

ipcMain.handle("capture:stop", () => {
  stopCaptureClient();
  return { ok: true };
});
