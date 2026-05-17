import { contextBridge, ipcRenderer } from "electron";

contextBridge.exposeInMainWorld("noteit", {
  minimize: () => ipcRenderer.invoke("window:minimize"),
  close: () => ipcRenderer.invoke("window:close"),
  openExternal: (url) => ipcRenderer.invoke("shell:openExternal", url),
});
