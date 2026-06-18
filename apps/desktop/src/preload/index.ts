import { contextBridge, ipcRenderer } from "electron";
import type { UpdateCheckResult } from "../shared/updates";

export interface EngineEventEnvelope {
  method: string;
  params: unknown;
}

const api = {
  openRepository: (): Promise<string | null> => ipcRenderer.invoke("dialog.openRepository"),
  request: <T>(method: string, params?: unknown): Promise<T> => ipcRenderer.invoke("engine.request", method, params),
  revealArtifact: (artifactId: string): Promise<void> => ipcRenderer.invoke("artifact.reveal", artifactId),
  getAppInfo: (): Promise<{ version: string; packaged: boolean }> => ipcRenderer.invoke("app.info"),
  checkForUpdates: (): Promise<UpdateCheckResult> => ipcRenderer.invoke("update.check"),
  openLatestUpdate: (): Promise<void> => ipcRenderer.invoke("update.openLatest"),
  onEngineEvent: (callback: (event: EngineEventEnvelope) => void) => {
    const listener = (_event: Electron.IpcRendererEvent, payload: EngineEventEnvelope) => callback(payload);
    ipcRenderer.on("engine.event", listener);
    return () => ipcRenderer.removeListener("engine.event", listener);
  },
};

contextBridge.exposeInMainWorld("piper", api);
