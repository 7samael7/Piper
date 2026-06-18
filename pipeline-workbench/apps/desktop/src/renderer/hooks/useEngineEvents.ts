import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { RunEvent } from "@pipeline-workbench/shared-types";
import { useWorkbenchStore } from "../store/workbenchStore";

export function useEngineEvents() {
  const appendRunEvent = useWorkbenchStore((state) => state.appendRunEvent);
  const setActiveRunId = useWorkbenchStore((state) => state.setActiveRunId);
  const queryClient = useQueryClient();

  useEffect(() => {
    return window.pipelineWorkbench.onEngineEvent((event) => {
      if (event.method !== "run.event") {
        return;
      }
      const runEvent = event.params as RunEvent;
      appendRunEvent(runEvent);
      if (runEvent.type === "run_finished" || runEvent.type === "run_failed" || runEvent.type === "run_cancelled") {
        setActiveRunId("");
        void queryClient.invalidateQueries({ queryKey: ["history"] });
      }
    });
  }, [appendRunEvent, queryClient, setActiveRunId]);
}
