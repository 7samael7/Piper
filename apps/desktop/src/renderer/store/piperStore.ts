import { create } from "zustand";
import type { ProviderID, RunEvent } from "@piper/shared-types";

interface PiperState {
  repoPath: string;
  selectedProvider: ProviderID;
  selectedWorkflowPath: string;
  selectedJobId: string;
  activeRunId: string;
  runEvents: RunEvent[];
  jobStates: Record<string, ExecutionState>;
  stepStates: Record<string, ExecutionState>;
  setRepoPath: (repoPath: string) => void;
  setSelectedProvider: (provider: ProviderID) => void;
  setSelectedWorkflowPath: (workflowPath: string) => void;
  setSelectedJobId: (jobId: string) => void;
  setActiveRunId: (runId: string) => void;
  appendRunEvent: (event: RunEvent) => void;
  clearRunEvents: () => void;
}

export interface ExecutionState {
  status: RunEvent["status"];
  message: string;
  condition?: {
    expression?: string;
    value?: boolean;
    reason?: string;
    error?: unknown;
  };
}

export const usePiperStore = create<PiperState>((set) => ({
  repoPath: "",
  selectedProvider: "github",
  selectedWorkflowPath: "",
  selectedJobId: "",
  activeRunId: "",
  runEvents: [],
  jobStates: {},
  stepStates: {},
  setRepoPath: (repoPath) =>
    set({
      repoPath,
      selectedWorkflowPath: "",
      selectedJobId: "",
      activeRunId: "",
      runEvents: [],
      jobStates: {},
      stepStates: {},
    }),
  setSelectedProvider: (selectedProvider) =>
    set({
      selectedProvider,
      selectedWorkflowPath: "",
      selectedJobId: "",
      activeRunId: "",
      runEvents: [],
      jobStates: {},
      stepStates: {},
    }),
  setSelectedWorkflowPath: (selectedWorkflowPath) =>
    set({
      selectedWorkflowPath,
      selectedJobId: "",
      runEvents: [],
      jobStates: {},
      stepStates: {},
    }),
  setSelectedJobId: (selectedJobId) => set({ selectedJobId }),
  setActiveRunId: (activeRunId) => set({ activeRunId }),
  appendRunEvent: (event) =>
    set((state) => {
      const jobStates = { ...state.jobStates };
      const stepStates = { ...state.stepStates };
      if (event.jobId) {
        const key = event.stepId ? `${event.jobId}/${event.stepId}` : event.jobId;
        const current = event.stepId ? stepStates[key] : jobStates[key];
        const next: ExecutionState = {
          status: event.status ?? current?.status,
          message: event.message,
          condition: current?.condition,
        };
        if (event.type === "condition_evaluated") {
          next.condition = {
            expression: String(event.data?.expression ?? ""),
            value: Boolean(event.data?.value),
            reason: String(event.data?.reason ?? event.message),
            error: event.data?.error,
          };
        }
        if (event.stepId) {
          stepStates[key] = next;
        } else {
          jobStates[key] = next;
        }
      }
      return { runEvents: [...state.runEvents, event], jobStates, stepStates };
    }),
  clearRunEvents: () => set({ runEvents: [], jobStates: {}, stepStates: {} }),
}));
