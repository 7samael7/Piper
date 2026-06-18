import { History } from "lucide-react";
import type { RunRecord } from "@piper/shared-types";

interface RunHistoryProps {
  runs: RunRecord[];
}

export function RunHistory({ runs }: RunHistoryProps) {
  return (
    <section className="history-panel">
      <div className="sidebar-heading">
        <History size={16} />
        <span>Run History</span>
      </div>
      <div className="history-list">
        {runs.length === 0 ? (
          <p className="muted">No local runs yet.</p>
        ) : (
          runs.map((run) => (
            <div className="history-row" key={run.id}>
              <div>
                <strong>{run.workflowPath.split("/").pop()}</strong>
                <span>{run.jobId || "workflow"} · {new Date(run.startedAt).toLocaleString()}</span>
              </div>
              <span className={`status-pill status-${run.status}`}>{run.status}</span>
            </div>
          ))
        )}
      </div>
    </section>
  );
}
