import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { FolderOpen, GitBranch, Play, RefreshCw, Square, Workflow } from "lucide-react";
import type { ProviderID, ProviderInfo, RunRecord, RunStartResponse, WorkflowDetails, WorkflowSummary } from "@pipeline-workbench/shared-types";
import { useEngineEvents } from "./hooks/useEngineEvents";
import { useWorkbenchStore } from "./store/workbenchStore";
import { JobInspector } from "./components/JobInspector";
import { KeyValueEditor, parseKeyValueText } from "./components/KeyValueEditor";
import { LogTerminal } from "./components/LogTerminal";
import { RunHistory } from "./components/RunHistory";
import { SupportBadge } from "./components/SupportBadge";
import { WorkflowGraph } from "./components/WorkflowGraph";

export function App() {
  useEngineEvents();
  const repoPath = useWorkbenchStore((state) => state.repoPath);
  const selectedProvider = useWorkbenchStore((state) => state.selectedProvider);
  const selectedWorkflowPath = useWorkbenchStore((state) => state.selectedWorkflowPath);
  const selectedJobId = useWorkbenchStore((state) => state.selectedJobId);
  const activeRunId = useWorkbenchStore((state) => state.activeRunId);
  const runEvents = useWorkbenchStore((state) => state.runEvents);
  const setRepoPath = useWorkbenchStore((state) => state.setRepoPath);
  const setSelectedProvider = useWorkbenchStore((state) => state.setSelectedProvider);
  const setSelectedWorkflowPath = useWorkbenchStore((state) => state.setSelectedWorkflowPath);
  const setSelectedJobId = useWorkbenchStore((state) => state.setSelectedJobId);
  const setActiveRunId = useWorkbenchStore((state) => state.setActiveRunId);
  const clearRunEvents = useWorkbenchStore((state) => state.clearRunEvents);

  const [eventName, setEventName] = useState("workflow_dispatch");
  const [inputsText, setInputsText] = useState("");
  const [envText, setEnvText] = useState("");
  const [secretsText, setSecretsText] = useState("");
  const [error, setError] = useState("");

  const providersQuery = useQuery({
    queryKey: ["providers"],
    queryFn: () => window.pipelineWorkbench.request<ProviderInfo[]>("provider.list"),
  });

  const workflowsQuery = useQuery({
    queryKey: ["workflows", repoPath, selectedProvider],
    enabled: Boolean(repoPath),
    queryFn: () =>
      window.pipelineWorkbench.request<WorkflowSummary[]>("workflow.discover", {
        repoPath,
        provider: selectedProvider,
      }),
  });

  useEffect(() => {
    setEventName(defaultEventName(selectedProvider));
  }, [selectedProvider]);

  useEffect(() => {
    if (!selectedWorkflowPath && workflowsQuery.data?.length) {
      setSelectedWorkflowPath(workflowsQuery.data[0].path);
    }
  }, [selectedWorkflowPath, setSelectedWorkflowPath, workflowsQuery.data]);

  const workflowQuery = useQuery({
    queryKey: ["workflow", repoPath, selectedProvider, selectedWorkflowPath],
    enabled: Boolean(repoPath && selectedWorkflowPath),
    queryFn: () =>
      window.pipelineWorkbench.request<WorkflowDetails>("workflow.get", {
        repoPath,
        workflowPath: selectedWorkflowPath,
        provider: selectedProvider,
      }),
  });

  const historyQuery = useQuery({
    queryKey: ["history", repoPath],
    enabled: Boolean(repoPath),
    queryFn: () =>
      window.pipelineWorkbench.request<RunRecord[]>("run.history", {
        repoPath,
        limit: 25,
      }),
  });

  const selectedWorkflow = workflowQuery.data;
  const selectedJob = useMemo(
    () => selectedWorkflow?.jobs.find((job) => job.id === selectedJobId),
    [selectedJobId, selectedWorkflow],
  );

  const runMutation = useMutation({
    mutationFn: async () => {
      if (!repoPath || !selectedWorkflowPath) {
        throw new Error("A repository and workflow are required.");
      }
      clearRunEvents();
      const response = await window.pipelineWorkbench.request<RunStartResponse>("run.start", {
        repoPath,
        workflowPath: selectedWorkflowPath,
        provider: selectedProvider,
        jobId: selectedJobId || undefined,
        eventName,
        inputs: parseKeyValueText(inputsText),
        env: parseKeyValueText(envText),
        secrets: parseKeyValueText(secretsText),
      });
      setActiveRunId(response.runId);
      return response;
    },
    onError: (mutationError) => {
      setError(mutationError instanceof Error ? mutationError.message : String(mutationError));
    },
  });

  const cancelMutation = useMutation({
    mutationFn: async () => {
      if (!activeRunId) {
        return;
      }
      await window.pipelineWorkbench.request("run.cancel", { runId: activeRunId });
    },
  });

  const openRepository = async () => {
    const selectedPath = await window.pipelineWorkbench.openRepository();
    if (selectedPath) {
      setError("");
      setRepoPath(selectedPath);
    }
  };

  const providers = providersQuery.data ?? fallbackProviders;
  const providerLabel = providers.find((provider) => provider.id === selectedProvider)?.name ?? selectedProvider;

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand-row">
          <GitBranch size={21} />
          <span>Pipeline Workbench</span>
        </div>
        <button className="primary-button" onClick={openRepository}>
          <FolderOpen size={17} />
          Open Repository
        </button>
        <div className="repo-path" title={repoPath}>
          {repoPath || "No repository selected"}
        </div>
        <div className="provider-switcher" role="tablist" aria-label="CI provider">
          {providers.map((provider) => (
            <button
              key={provider.id}
              className={provider.id === selectedProvider ? "provider-tab active" : "provider-tab"}
              onClick={() => setSelectedProvider(provider.id)}
              title={provider.description}
            >
              {providerLabelShort(provider.id)}
            </button>
          ))}
        </div>

        <section className="workflow-panel">
          <div className="sidebar-heading">
            <Workflow size={16} />
            <span>Workflows</span>
            {workflowsQuery.isFetching ? <RefreshCw className="spin" size={14} /> : null}
          </div>
          <div className="workflow-list">
            {(workflowsQuery.data ?? []).map((workflow) => (
              <button
                key={`${workflow.provider}:${workflow.path}`}
                className={workflow.path === selectedWorkflowPath ? "workflow-row active" : "workflow-row"}
                onClick={() => setSelectedWorkflowPath(workflow.path)}
              >
                <span>
                  {workflow.name}
                  <small>{workflow.provider}</small>
                </span>
                <SupportBadge support={workflow.support} />
              </button>
            ))}
            {repoPath && !workflowsQuery.isFetching && workflowsQuery.data?.length === 0 ? (
              <p className="muted">No {providerLabel} workflows found.</p>
            ) : null}
          </div>
        </section>

        <RunHistory runs={historyQuery.data ?? []} />
      </aside>

      <main className="main-shell">
        <header className="topbar">
          <div>
            <h1>{selectedWorkflow?.name ?? "Local CI/CD Workbench"}</h1>
            <span>{selectedWorkflow?.path ?? `${providerLabel} provider`}</span>
          </div>
          <div className="run-actions">
            <label className="event-field">
              <span>Event</span>
              <input value={eventName} onChange={(event) => setEventName(event.target.value)} />
            </label>
            <button
              className="secondary-button"
              disabled={!selectedWorkflow || Boolean(activeRunId) || runMutation.isPending}
              onClick={() => runMutation.mutate()}
              title={selectedJob ? `Run ${selectedJob.name}` : "Run workflow"}
            >
              <Play size={16} />
              {selectedJob ? "Run Job" : "Run Workflow"}
            </button>
            <button className="danger-button" disabled={!activeRunId} onClick={() => cancelMutation.mutate()} title="Cancel active run">
              <Square size={15} />
              Cancel
            </button>
          </div>
        </header>

        {error ? <div className="error-banner">{error}</div> : null}

        <section className="config-strip">
          <KeyValueEditor label="Inputs" value={inputsText} onChange={setInputsText} />
          <KeyValueEditor label="Environment" value={envText} onChange={setEnvText} />
          <KeyValueEditor label="Secrets" value={secretsText} onChange={setSecretsText} secret />
        </section>

        <section className="workspace-grid">
          <div className="graph-panel">
            <WorkflowGraph workflow={selectedWorkflow} selectedJobId={selectedJobId} onSelectJob={setSelectedJobId} />
          </div>
          <JobInspector workflow={selectedWorkflow} selectedJobId={selectedJobId} />
        </section>

        <section className="logs-panel">
          <div className="logs-heading">
            <span>Live Logs</span>
            {activeRunId ? <strong>{activeRunId}</strong> : <span className="muted">idle</span>}
          </div>
          <LogTerminal events={runEvents} />
        </section>
      </main>
    </div>
  );
}

const fallbackProviders: ProviderInfo[] = [
  {
    id: "github",
    name: "GitHub Actions",
    description: "GitHub Actions workflows",
    capabilities: [],
  },
  {
    id: "gitlab",
    name: "GitLab CI/CD",
    description: "GitLab CI/CD pipelines",
    capabilities: [],
  },
  {
    id: "azure",
    name: "Azure Pipelines",
    description: "Azure Pipelines YAML",
    capabilities: [],
  },
];

function defaultEventName(provider: ProviderID) {
  switch (provider) {
    case "gitlab":
      return "web";
    case "azure":
      return "manual";
    default:
      return "workflow_dispatch";
  }
}

function providerLabelShort(provider: ProviderID) {
  switch (provider) {
    case "gitlab":
      return "GitLab";
    case "azure":
      return "Azure";
    default:
      return "GitHub";
  }
}
