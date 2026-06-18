import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Download, FolderOpen, GitBranch, Play, RefreshCw, Square, Workflow } from "lucide-react";
import type { ProviderID, ProviderInfo, RunRecord, RunStartResponse, WorkflowDetails, WorkflowSummary } from "@piper/shared-types";
import { useEngineEvents } from "./hooks/useEngineEvents";
import { usePiperStore } from "./store/piperStore";
import { JobInspector } from "./components/JobInspector";
import { KeyValueEditor, parseKeyValueText } from "./components/KeyValueEditor";
import { LogTerminal } from "./components/LogTerminal";
import { RunHistory } from "./components/RunHistory";
import { SupportBadge } from "./components/SupportBadge";
import { WorkflowGraph } from "./components/WorkflowGraph";

export function App() {
  useEngineEvents();
  const repoPath = usePiperStore((state) => state.repoPath);
  const selectedProvider = usePiperStore((state) => state.selectedProvider);
  const selectedWorkflowPath = usePiperStore((state) => state.selectedWorkflowPath);
  const selectedJobId = usePiperStore((state) => state.selectedJobId);
  const activeRunId = usePiperStore((state) => state.activeRunId);
  const runEvents = usePiperStore((state) => state.runEvents);
  const setRepoPath = usePiperStore((state) => state.setRepoPath);
  const setSelectedProvider = usePiperStore((state) => state.setSelectedProvider);
  const setSelectedWorkflowPath = usePiperStore((state) => state.setSelectedWorkflowPath);
  const setSelectedJobId = usePiperStore((state) => state.setSelectedJobId);
  const setActiveRunId = usePiperStore((state) => state.setActiveRunId);
  const clearRunEvents = usePiperStore((state) => state.clearRunEvents);

  const [eventName, setEventName] = useState("workflow_dispatch");
  const [inputsText, setInputsText] = useState("");
  const [envText, setEnvText] = useState("");
  const [secretsText, setSecretsText] = useState("");
  const [error, setError] = useState("");

  const providersQuery = useQuery({
    queryKey: ["providers"],
    queryFn: () => window.piper.request<ProviderInfo[]>("provider.list"),
  });

  const appInfoQuery = useQuery({
    queryKey: ["app-info"],
    queryFn: () => window.piper.getAppInfo(),
    staleTime: Number.POSITIVE_INFINITY,
  });

  const updateQuery = useQuery({
    queryKey: ["app-update"],
    queryFn: () => window.piper.checkForUpdates(),
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 15 * 60 * 1000,
  });

  const openUpdateMutation = useMutation({
    mutationFn: () => window.piper.openLatestUpdate(),
    onError: (mutationError) => {
      setError(mutationError instanceof Error ? mutationError.message : String(mutationError));
    },
  });

  const workflowsQuery = useQuery({
    queryKey: ["workflows", repoPath, selectedProvider],
    enabled: Boolean(repoPath),
    queryFn: () =>
      window.piper.request<WorkflowSummary[]>("workflow.discover", {
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
      window.piper.request<WorkflowDetails>("workflow.get", {
        repoPath,
        workflowPath: selectedWorkflowPath,
        provider: selectedProvider,
      }),
  });

  const historyQuery = useQuery({
    queryKey: ["history", repoPath],
    enabled: Boolean(repoPath),
    queryFn: () =>
      window.piper.request<RunRecord[]>("run.history", {
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
      const response = await window.piper.request<RunStartResponse>("run.start", {
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
      await window.piper.request("run.cancel", { runId: activeRunId });
    },
  });

  const openRepository = async () => {
    const selectedPath = await window.piper.openRepository();
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
          <span>Piper</span>
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
        <div className="app-footer">
          <span>v{appInfoQuery.data?.version ?? "0.1.0"}</span>
          {updateQuery.data?.status === "available" ? (
            <button
              className="update-button"
              onClick={() => openUpdateMutation.mutate()}
              disabled={openUpdateMutation.isPending}
              title={updateQuery.data.message}
            >
              <Download size={14} />
              Install v{updateQuery.data.latestVersion}
            </button>
          ) : (
            <button
              className="icon-button"
              onClick={() => void updateQuery.refetch()}
              disabled={updateQuery.isFetching}
              title={updateQuery.data?.message ?? "Check for updates"}
              aria-label="Check for updates"
            >
              <RefreshCw className={updateQuery.isFetching ? "spin" : undefined} size={15} />
            </button>
          )}
        </div>
      </aside>

      <main className="main-shell">
        <header className="topbar">
          <div>
            <h1>{selectedWorkflow?.name ?? "Piper"}</h1>
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
