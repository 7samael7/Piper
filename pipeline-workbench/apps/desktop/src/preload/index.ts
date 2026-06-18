import { contextBridge, ipcRenderer } from "electron";

export interface EngineEventEnvelope {
  method: string;
  params: unknown;
}

const api = {
  openRepository: (): Promise<string | null> => ipcRenderer.invoke("dialog.openRepository"),
  request: <T>(method: string, params?: unknown): Promise<T> => ipcRenderer.invoke("engine.request", method, params),
  onEngineEvent: (callback: (event: EngineEventEnvelope) => void) => {
    const listener = (_event: Electron.IpcRendererEvent, payload: EngineEventEnvelope) => callback(payload);
    ipcRenderer.on("engine.event", listener);
    return () => ipcRenderer.removeListener("engine.event", listener);
  },
};

contextBridge.exposeInMainWorld("pipelineWorkbench", api);
