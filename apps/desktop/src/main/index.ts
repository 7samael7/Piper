import { app, BrowserWindow, dialog, ipcMain, shell } from "electron";
import { ChildProcessWithoutNullStreams, spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import readline from "node:readline";
import { UpdateService } from "./updates";

declare const MAIN_WINDOW_VITE_DEV_SERVER_URL: string | undefined;
declare const MAIN_WINDOW_VITE_NAME: string;

interface JsonRpcError {
  code: number;
  message: string;
  data?: unknown;
}

interface PendingRequest {
  resolve: (value: unknown) => void;
  reject: (error: Error) => void;
}

class EngineClient {
  private child?: ChildProcessWithoutNullStreams;
  private pending = new Map<number, PendingRequest>();
  private nextID = 1;

  start() {
    if (this.child) {
      return;
    }
    const command = resolveEngineCommand();
    this.child = spawn(command.command, command.args, {
      cwd: command.cwd,
      stdio: ["pipe", "pipe", "pipe"],
      env: {
        ...process.env,
        PIPER_DB: process.env.PIPER_DB ?? defaultDatabasePath(),
      },
    });

    const stdout = readline.createInterface({ input: this.child.stdout });
    stdout.on("line", (line) => this.handleLine(line));
    this.child.stderr.on("data", (chunk) => {
      console.error(`[engine] ${chunk.toString()}`);
    });
    this.child.on("exit", (code, signal) => {
      const error = new Error(`Engine exited with code ${code ?? "null"} and signal ${signal ?? "null"}`);
      for (const request of this.pending.values()) {
        request.reject(error);
      }
      this.pending.clear();
      this.child = undefined;
      broadcastEngineEvent("engine.exit", { code, signal });
    });
  }

  stop() {
    if (!this.child) {
      return;
    }
    this.child.kill();
    this.child = undefined;
  }

  request<T>(method: string, params?: unknown): Promise<T> {
    this.start();
    if (!this.child) {
      return Promise.reject(new Error("Engine process is not available."));
    }
    const id = this.nextID++;
    const payload = JSON.stringify({
      jsonrpc: "2.0",
      id,
      method,
      params: params ?? {},
    });
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, {
        resolve: (value) => resolve(value as T),
        reject,
      });
      this.child?.stdin.write(payload + "\n", (error) => {
        if (error) {
          this.pending.delete(id);
          reject(error);
        }
      });
    });
  }

  private handleLine(line: string) {
    if (!line.trim()) {
      return;
    }
    let message: {
      id?: number;
      method?: string;
      result?: unknown;
      error?: JsonRpcError;
      params?: unknown;
    };
    try {
      message = JSON.parse(line);
    } catch (error) {
      console.error("[engine] invalid JSON", error, line);
      return;
    }

    if (message.method) {
      broadcastEngineEvent(message.method, message.params);
      return;
    }
    if (typeof message.id === "number") {
      const pending = this.pending.get(message.id);
      if (!pending) {
        return;
      }
      this.pending.delete(message.id);
      if (message.error) {
        pending.reject(new Error(message.error.message));
      } else {
        pending.resolve(message.result);
      }
    }
  }
}

const engine = new EngineClient();
const updates = new UpdateService();

function createWindow() {
  const mainWindow = new BrowserWindow({
    width: 1440,
    height: 980,
    minWidth: 900,
    minHeight: 620,
    title: "Piper",
    backgroundColor: "#f7f8f3",
    icon: resolveWindowIcon(),
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  if (MAIN_WINDOW_VITE_DEV_SERVER_URL) {
    mainWindow.loadURL(MAIN_WINDOW_VITE_DEV_SERVER_URL);
  } else {
    mainWindow.loadFile(path.join(__dirname, `../renderer/${MAIN_WINDOW_VITE_NAME}/index.html`));
  }
}

app.whenReady().then(() => {
  setApplicationIcon();
  engine.start();
  ipcMain.handle("dialog.openRepository", async () => {
    const result = await dialog.showOpenDialog({
      title: "Open Git repository",
      properties: ["openDirectory"],
    });
    return result.canceled ? null : result.filePaths[0];
  });
  ipcMain.handle("engine.request", async (_event, method: string, params?: unknown) => {
    return engine.request(method, params);
  });
  ipcMain.handle("artifact.reveal", async (_event, artifactId: string) => {
    const artifacts = await engine.request<Array<{ id: string; path: string }>>("artifact.list", {});
    const artifact = artifacts.find((item) => item.id === artifactId);
    if (!artifact) {
      throw new Error("Artifact was not found.");
    }
    shell.showItemInFolder(artifact.path);
  });
  ipcMain.handle("app.info", () => ({
    version: app.getVersion(),
    packaged: app.isPackaged,
  }));
  ipcMain.handle("update.check", () => updates.check());
  ipcMain.handle("update.openLatest", () => updates.openLatest());

  createWindow();
  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    }
  });
});

app.on("before-quit", () => {
  engine.stop();
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

function broadcastEngineEvent(method: string, params: unknown) {
  for (const window of BrowserWindow.getAllWindows()) {
    window.webContents.send("engine.event", { method, params });
  }
}

function resolveEngineCommand(): { command: string; args: string[]; cwd?: string } {
  const packagedBinary = path.join(process.resourcesPath, process.platform === "win32" ? "piper-engine.exe" : "piper-engine");
  if (app.isPackaged && fs.existsSync(packagedBinary)) {
    return { command: packagedBinary, args: [] };
  }

  const repoRoot = path.resolve(process.cwd(), "../..");
  const devBinary = path.join(repoRoot, "engine", "bin", process.platform === "win32" ? "piper-engine.exe" : "piper-engine");
  if (fs.existsSync(devBinary)) {
    return { command: devBinary, args: [] };
  }

  return {
    command: "go",
    args: ["run", "./cmd/daemon"],
    cwd: path.join(repoRoot, "engine"),
  };
}

function defaultDatabasePath() {
  return path.join(app.getPath("userData"), "piper.db");
}

function resolveWindowIcon() {
  return app.isPackaged
    ? path.join(process.resourcesPath, "piper-app.png")
    : path.resolve(__dirname, "../../assets/piper-app.png");
}

function setApplicationIcon() {
  if (process.platform === "darwin") {
    app.dock?.setIcon(resolveWindowIcon());
  }
}
