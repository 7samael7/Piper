import type { EngineEventEnvelope } from "../preload";

declare global {
  interface Window {
    pipelineWorkbench: {
      openRepository: () => Promise<string | null>;
      request: <T>(method: string, params?: unknown) => Promise<T>;
      onEngineEvent: (callback: (event: EngineEventEnvelope) => void) => () => void;
    };
  }
}

export {};
