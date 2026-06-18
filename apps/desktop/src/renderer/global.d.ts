import type { EngineEventEnvelope } from "../preload";
import type { UpdateCheckResult } from "../shared/updates";

declare global {
  interface Window {
    piper: {
      openRepository: () => Promise<string | null>;
      request: <T>(method: string, params?: unknown) => Promise<T>;
      revealArtifact: (artifactId: string) => Promise<void>;
      getAppInfo: () => Promise<{ version: string; packaged: boolean }>;
      checkForUpdates: () => Promise<UpdateCheckResult>;
      openLatestUpdate: () => Promise<void>;
      onEngineEvent: (callback: (event: EngineEventEnvelope) => void) => () => void;
    };
  }
}

export {};
