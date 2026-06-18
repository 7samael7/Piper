import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { RunEvent } from "@piper/shared-types";
import { usePiperStore } from "../store/piperStore";

export function useEngineEvents() {
  const appendRunEvent = usePiperStore((state) => state.appendRunEvent);
  const setActiveRunId = usePiperStore((state) => state.setActiveRunId);
  const queryClient = useQueryClient();

  useEffect(() => {
    return window.piper.onEngineEvent((event) => {
      if (event.method !== "run.event") {
        return;
      }
      const runEvent = event.params as RunEvent;
      appendRunEvent(runEvent);
      if (runEvent.type.startsWith("artifact_")) {
        void queryClient.invalidateQueries({ queryKey: ["artifacts"] });
      }
      if (runEvent.type.startsWith("cache_")) {
        void queryClient.invalidateQueries({ queryKey: ["caches"] });
      }
      if (runEvent.type === "run_finished" || runEvent.type === "run_failed" || runEvent.type === "run_cancelled") {
        setActiveRunId("");
        void queryClient.invalidateQueries({ queryKey: ["history"] });
      }
    });
  }, [appendRunEvent, queryClient, setActiveRunId]);
}
