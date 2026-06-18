import { create } from "zustand";
import type { ProviderID, RunEvent } from "@piper/shared-types";

interface PiperState {
  repoPath: string;
  selectedProvider: ProviderID;
  selectedWorkflowPath: string;
  selectedJobId: string;
  activeRunId: string;
  runEvents: RunEvent[];
  setRepoPath: (repoPath: string) => void;
  setSelectedProvider: (provider: ProviderID) => void;
  setSelectedWorkflowPath: (workflowPath: string) => void;
  setSelectedJobId: (jobId: string) => void;
  setActiveRunId: (runId: string) => void;
  appendRunEvent: (event: RunEvent) => void;
  clearRunEvents: () => void;
}

export const usePiperStore = create<PiperState>((set) => ({
  repoPath: "",
  selectedProvider: "github",
  selectedWorkflowPath: "",
  selectedJobId: "",
  activeRunId: "",
  runEvents: [],
  setRepoPath: (repoPath) =>
    set({
      repoPath,
      selectedWorkflowPath: "",
      selectedJobId: "",
      activeRunId: "",
      runEvents: [],
    }),
  setSelectedProvider: (selectedProvider) =>
    set({
      selectedProvider,
      selectedWorkflowPath: "",
      selectedJobId: "",
      activeRunId: "",
      runEvents: [],
    }),
  setSelectedWorkflowPath: (selectedWorkflowPath) =>
    set({
      selectedWorkflowPath,
      selectedJobId: "",
      runEvents: [],
    }),
  setSelectedJobId: (selectedJobId) => set({ selectedJobId }),
  setActiveRunId: (activeRunId) => set({ activeRunId }),
  appendRunEvent: (event) =>
    set((state) => ({
      runEvents: [...state.runEvents, event],
    })),
  clearRunEvents: () => set({ runEvents: [] }),
}));
