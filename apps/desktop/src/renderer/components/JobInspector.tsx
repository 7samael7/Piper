import { useMemo, useState } from "react";
import Editor from "@monaco-editor/react";
import { AlertTriangle, CheckCircle2, FileCode2, ListChecks } from "lucide-react";
import type { PipelineJob, WorkflowDetails } from "@piper/shared-types";
import { SupportBadge } from "./SupportBadge";
import type { ExecutionState } from "../store/piperStore";

interface JobInspectorProps {
  workflow?: WorkflowDetails;
  selectedJobId: string;
  jobStates: Record<string, ExecutionState>;
  stepStates: Record<string, ExecutionState>;
}

export function JobInspector({ workflow, selectedJobId, jobStates, stepStates }: JobInspectorProps) {
  const [tab, setTab] = useState<"job" | "yaml" | "resolved">("job");
  const selectedJob = useMemo(
    () => {
      const logicalId = workflow?.executionPlan?.jobs.find((job) => job.id === selectedJobId)?.logicalJobId ?? selectedJobId;
      return workflow?.jobs.find((job) => job.id === logicalId) ?? workflow?.jobs[0];
    },
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
        {workflow.resolvedYaml ? (
          <button className={tab === "resolved" ? "tab active" : "tab"} onClick={() => setTab("resolved")}>
            <FileCode2 size={16} />
            Resolved
          </button>
        ) : null}
      </div>

      {tab === "yaml" || tab === "resolved" ? (
        <div className="editor-shell">
          <Editor
            value={tab === "resolved" ? workflow.resolvedYaml : workflow.rawYaml}
            language="yaml"
            theme="vs-light"
            options={{
              readOnly: true,
              minimap: { enabled: false },
              fontSize: 11,
              scrollBeyondLastLine: false,
              wordWrap: "on",
            }}
          />
        </div>
      ) : (
        <div className="inspector-scroll">
          <WorkflowSummary workflow={workflow} />
          {selectedJob ? (
            <JobDetails job={selectedJob} state={jobStates[selectedJobId || selectedJob.id]} stepStates={stepStates} stateJobId={selectedJobId || selectedJob.id} />
          ) : <div className="empty-state">No job selected.</div>}
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

function JobDetails({ job, state, stepStates, stateJobId }: { job: PipelineJob; state?: ExecutionState; stepStates: Record<string, ExecutionState>; stateJobId: string }) {
  return (
    <section className="inspector-section">
      <div className="section-title">
        <span>{job.name}</span>
        <SupportBadge support={job.support} />
      </div>
      <div className="meta-grid">
        <span>Status</span>
        <strong>{state?.status ?? "not run"}</strong>
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
        {state?.condition ? (
          <>
            <span>Condition</span>
            <strong title={state.condition.expression}>{state.condition.reason}</strong>
          </>
        ) : null}
      </div>
      <div className="step-list">
        {job.steps.map((step, index) => (
          <div className="step-row" key={`${step.id || step.name}-${index}`}>
            <div>
              <strong>{step.name}</strong>
              <span>
                {stepStates[`${stateJobId}/${step.id || `step-${index + 1}`}`]?.status ?? (step.run ? "run" : step.uses || "empty")}
              </span>
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
          <div className="feature-row" key={`${feature.featureId}-${feature.path}`}>
            <div>
              <strong title={feature.featureId}>{feature.feature}</strong>
              <span>{feature.localBehavior}</span>
              {feature.hostedDifferences ? <span>Hosted difference: {feature.hostedDifferences}</span> : null}
              {feature.fallback ? <span>Fallback: {feature.fallback}</span> : null}
            </div>
            <SupportBadge support={feature.support} />
          </div>
        ))}
      </div>
    </section>
  );
}
