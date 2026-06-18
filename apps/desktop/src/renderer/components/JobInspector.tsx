import { useMemo, useState } from "react";
import Editor from "@monaco-editor/react";
import { AlertTriangle, CheckCircle2, FileCode2, ListChecks } from "lucide-react";
import type { PipelineJob, WorkflowDetails } from "@piper/shared-types";
import { SupportBadge } from "./SupportBadge";

interface JobInspectorProps {
  workflow?: WorkflowDetails;
  selectedJobId: string;
}

export function JobInspector({ workflow, selectedJobId }: JobInspectorProps) {
  const [tab, setTab] = useState<"job" | "yaml">("job");
  const selectedJob = useMemo(
    () => workflow?.jobs.find((job) => job.id === selectedJobId) ?? workflow?.jobs[0],
    [selectedJobId, workflow],
  );

  if (!workflow) {
    return <aside className="inspector-panel empty-state">No workflow selected.</aside>;
  }

  return (
    <aside className="inspector-panel">
      <div className="tabs">
        <button className={tab === "job" ? "tab active" : "tab"} onClick={() => setTab("job")}>
          <ListChecks size={16} />
          Job
        </button>
        <button className={tab === "yaml" ? "tab active" : "tab"} onClick={() => setTab("yaml")}>
          <FileCode2 size={16} />
          YAML
        </button>
      </div>

      {tab === "yaml" ? (
        <div className="editor-shell">
          <Editor
            value={workflow.rawYaml}
            language="yaml"
            theme="vs-light"
            options={{
              readOnly: true,
              minimap: { enabled: false },
              fontSize: 13,
              scrollBeyondLastLine: false,
              wordWrap: "on",
            }}
          />
        </div>
      ) : (
        <div className="inspector-scroll">
          <WorkflowSummary workflow={workflow} />
          {selectedJob ? <JobDetails job={selectedJob} /> : <div className="empty-state">No job selected.</div>}
          <ValidationDetails workflow={workflow} />
        </div>
      )}
    </aside>
  );
}

function WorkflowSummary({ workflow }: { workflow: WorkflowDetails }) {
  return (
    <section className="inspector-section">
      <div className="section-title">
        <span>{workflow.name}</span>
        <SupportBadge support={workflow.support} />
      </div>
      <div className="meta-grid">
        <span>Provider</span>
        <strong>{workflow.provider}</strong>
        <span>Jobs</span>
        <strong>{workflow.jobCount}</strong>
        <span>Path</span>
        <strong title={workflow.path}>{workflow.path}</strong>
      </div>
    </section>
  );
}

function JobDetails({ job }: { job: PipelineJob }) {
  return (
    <section className="inspector-section">
      <div className="section-title">
        <span>{job.name}</span>
        <SupportBadge support={job.support} />
      </div>
      <div className="meta-grid">
        <span>ID</span>
        <strong>{job.id}</strong>
        <span>Runs on</span>
        <strong>{job.runner || "not set"}</strong>
        <span>Stage</span>
        <strong>{job.stage || "not set"}</strong>
        <span>Image</span>
        <strong>{job.image || "default local image"}</strong>
        <span>Needs</span>
        <strong>{job.needs.length ? job.needs.join(", ") : "none"}</strong>
      </div>
      <div className="step-list">
        {job.steps.map((step, index) => (
          <div className="step-row" key={`${step.id || step.name}-${index}`}>
            <div>
              <strong>{step.name}</strong>
              <span>{step.run ? "run" : step.uses || "empty"}</span>
            </div>
            <SupportBadge support={step.support} />
          </div>
        ))}
      </div>
    </section>
  );
}

function ValidationDetails({ workflow }: { workflow: WorkflowDetails }) {
  return (
    <section className="inspector-section">
      <div className="section-title">
        <span>Validation</span>
        {workflow.validation.valid ? <CheckCircle2 size={17} /> : <AlertTriangle size={17} />}
      </div>
      <div className="issue-list">
        {workflow.validation.issues.length === 0 ? (
          <p className="muted">No blocking YAML or graph errors.</p>
        ) : (
          workflow.validation.issues.map((issue) => (
            <div className={`issue-row ${issue.severity}`} key={`${issue.code}-${issue.path}`}>
              <strong>{issue.code}</strong>
              <span>{issue.message}</span>
            </div>
          ))
        )}
      </div>
      <div className="feature-list">
        {workflow.validation.features.map((feature) => (
          <div className="feature-row" key={`${feature.feature}-${feature.path}`}>
            <div>
              <strong>{feature.feature}</strong>
              <span>{feature.message}</span>
            </div>
            <SupportBadge support={feature.support} />
          </div>
        ))}
      </div>
    </section>
  );
}
