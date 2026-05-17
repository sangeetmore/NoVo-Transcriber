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
  if (!args?.capture_session_id || !args?.client_token) {
    throw new Error("Missing capture_session_id or client_token");
  }

  stopCaptureClient();

  const backendDir = resolve(__dirname, "../../backend");
  const scriptPath = resolve(backendDir, "scripts/capture_client.py");
  const pythonBin = process.env.NOTEIT_PYTHON || process.env.PYTHON_BIN || "python";

  emitCaptureLog("info", `Starting Python capture client: ${pythonBin}`);
  emitCaptureLog("info", `Capture script: ${scriptPath}`);

  captureProcess = spawn(pythonBin, [scriptPath, args.capture_session_id, args.client_token], {
    cwd: backendDir,
    env: {
      ...process.env,
      CAPTURE_CLIENT_LISTEN_SECONDS: process.env.CAPTURE_CLIENT_LISTEN_SECONDS || "900",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });

  captureProcess.stdout?.on("data", (data) => {
    for (const line of data.toString().split(/\r?\n/).filter(Boolean)) {
      emitCaptureLog("info", line.trim());
    }
  });

  captureProcess.stderr?.on("data", (data) => {
    for (const line of data.toString().split(/\r?\n/).filter(Boolean)) {
      emitCaptureLog("error", line.trim());
    }
  });

  captureProcess.on("error", (error) => {
    emitCaptureLog("error", `Failed to start capture client: ${error.message}`);
    captureProcess = null;
  });

  captureProcess.on("exit", (code) => {
    emitCaptureLog(code === 0 ? "info" : "error", `Capture client exited with code ${code}`);
    captureProcess = null;
  });

  return { ok: true, pid: captureProcess.pid };
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
    title: "Note It",
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
  //   console.warn("[note-it] content protection unavailable:", error);
  // }

  mainWindow.loadURL("http://127.0.0.1:5173");
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
