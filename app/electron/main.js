import { app, BrowserWindow, ipcMain, shell } from "electron";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

let mainWindow = null;

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
      preload: join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  try {
    mainWindow.setContentProtection(true);
  } catch (error) {
    console.warn("[note-it] content protection unavailable:", error);
  }

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
  if (process.platform !== "darwin") {
    app.quit();
  }
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
