const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("novotranscriber", {
  minimize: () => ipcRenderer.invoke("window:minimize"),
  close: () => ipcRenderer.invoke("window:close"),
  openExternal: (url) => ipcRenderer.invoke("shell:openExternal", url),
  startCapture: (args) => ipcRenderer.invoke("capture:start", args),
  stopCapture: () => ipcRenderer.invoke("capture:stop"),
  onCaptureLog: (callback) => {
    const listener = (_event, payload) => callback(payload);
    ipcRenderer.on("capture:log", listener);
    return () => ipcRenderer.removeListener("capture:log", listener);
  },
});
