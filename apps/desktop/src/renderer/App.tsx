import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, FolderOpen, Play, RefreshCw, Square, Workflow } from "lucide-react";
import type {
  ArtifactRecord,
  CacheRecord,
  PiperSettings,
  ProviderID,
  ProviderInfo,
  RunPreparation,
  RunRecord,
  RunStartResponse,
  WorkflowDetails,
  WorkflowSummary,
} from "@piper/shared-types";
import piperLogo from "../../assets/piper.png";
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
  const queryClient = useQueryClient();
  const repoPath = usePiperStore((state) => state.repoPath);
  const selectedProvider = usePiperStore((state) => state.selectedProvider);
  const selectedWorkflowPath = usePiperStore((state) => state.selectedWorkflowPath);
  const selectedJobId = usePiperStore((state) => state.selectedJobId);
  const activeRunId = usePiperStore((state) => state.activeRunId);
  const runEvents = usePiperStore((state) => state.runEvents);
  const jobStates = usePiperStore((state) => state.jobStates);
  const stepStates = usePiperStore((state) => state.stepStates);
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

  const artifactsQuery = useQuery({
    queryKey: ["artifacts", activeRunId],
    queryFn: () => window.piper.request<ArtifactRecord[]>("artifact.list", activeRunId ? { runId: activeRunId } : {}),
  });

  const cachesQuery = useQuery({
    queryKey: ["caches"],
    queryFn: () => window.piper.request<CacheRecord[]>("cache.list", {}),
  });

  const settingsQuery = useQuery({
    queryKey: ["settings"],
    queryFn: () => window.piper.request<PiperSettings>("settings.get"),
  });

  const selectedWorkflow = workflowQuery.data;
  const selectedJob = useMemo(
    () => {
      const logicalId = selectedWorkflow?.executionPlan?.jobs.find((job) => job.id === selectedJobId)?.logicalJobId ?? selectedJobId;
      return selectedWorkflow?.jobs.find((job) => job.id === logicalId);
    },
    [selectedJobId, selectedWorkflow],
  );

  const runMutation = useMutation({
    mutationFn: async () => {
      if (!repoPath || !selectedWorkflowPath) {
        throw new Error("A repository and workflow are required.");
      }
      clearRunEvents();
      const baseRequest = {
        repoPath,
        workflowPath: selectedWorkflowPath,
        provider: selectedProvider,
        jobId: selectedJobId || undefined,
        eventName,
        inputs: parseKeyValueText(inputsText),
        env: parseKeyValueText(envText),
        secrets: parseKeyValueText(secretsText),
      };
      let preparation = await window.piper.request<RunPreparation>("run.prepare", baseRequest);
      if (preparation.requiresConsent) {
        const accepted = window.confirm(
          "This workflow contains third-party actions. Piper will download and execute their code locally. Continue?",
        );
        if (!accepted) {
          throw new Error("Third-party action execution was not approved.");
        }
        preparation = await window.piper.request<RunPreparation>("run.prepare", {
          ...baseRequest,
          allowThirdPartyCode: true,
        });
        if (selectedWorkflow && window.confirm("Remember these action references as trusted for this repository?")) {
          const references = selectedWorkflow.jobs
            .flatMap((job) => job.steps.map((step) => step.uses))
            .filter((uses): uses is string =>
              Boolean(uses && uses.includes("@") && !uses.startsWith("./") && !isBuiltinActionReference(uses)),
            );
          await Promise.all(references.map((reference) =>
            window.piper.request("trust.update", { repoPath, reference, trusted: true }),
          ));
        }
      }
      const response = await window.piper.request<RunStartResponse>("run.start", {
        ...baseRequest,
        preparedToken: preparation.preparedToken,
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

  const approvalMutation = useMutation({
    mutationFn: (approved: boolean) =>
      window.piper.request(approved ? "run.approve" : "run.reject", { runId: activeRunId }),
  });

  const clearCachesMutation = useMutation({
    mutationFn: () => window.piper.request("cache.clear", {}),
    onSuccess: () => void cachesQuery.refetch(),
  });

  const settingsMutation = useMutation({
    mutationFn: (settings: PiperSettings) => window.piper.request<PiperSettings>("settings.update", settings),
    onSuccess: (settings) => queryClient.setQueryData(["settings"], settings),
  });

  const pendingApproval = [...runEvents]
    .reverse()
    .find((event) => event.type === "approval_required" && !runEvents.some((later) =>
      later.time > event.time && (later.type === "approval_granted" || later.type === "job_skipped") && later.jobId === event.jobId,
    ));

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
          <img className="brand-logo" src={piperLogo} alt="" />
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
              Download v{updateQuery.data.latestVersion}
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

      <main className={error ? "main-shell has-error" : "main-shell"}>
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
            {pendingApproval ? (
              <>
                <button className="secondary-button" onClick={() => approvalMutation.mutate(true)}>Approve</button>
                <button className="danger-button" onClick={() => approvalMutation.mutate(false)}>Reject</button>
              </>
            ) : null}
          </div>
        </header>

        {error ? <div className="error-banner">{error}</div> : null}

        <section className="config-strip run-config-strip">
          <KeyValueEditor label="Inputs" value={inputsText} onChange={setInputsText} />
          <KeyValueEditor label="Environment" value={envText} onChange={setEnvText} />
          <KeyValueEditor label="Secrets" value={secretsText} onChange={setSecretsText} secret />
        </section>

        <section className="workspace-grid">
          <div className="graph-panel">
            <WorkflowGraph workflow={selectedWorkflow} selectedJobId={selectedJobId} onSelectJob={setSelectedJobId} jobStates={jobStates} />
          </div>
          <JobInspector workflow={selectedWorkflow} selectedJobId={selectedJobId} jobStates={jobStates} stepStates={stepStates} />
        </section>

        <section className="config-strip resources-strip">
          <div>
            <strong>Artifacts</strong>
            {(artifactsQuery.data ?? []).length === 0 ? <p className="muted">No local artifacts.</p> : null}
            {(artifactsQuery.data ?? []).map((artifact) => (
              <button className="secondary-button" key={artifact.id} onClick={() => window.piper.revealArtifact(artifact.id)}>
                {artifact.name} ({formatBytes(artifact.size)})
              </button>
            ))}
          </div>
          <div>
            <strong>Caches</strong>
            <p className="muted">{(cachesQuery.data ?? []).length} entries</p>
            <button className="secondary-button" onClick={() => clearCachesMutation.mutate()}>Clear caches</button>
          </div>
          <div>
            <strong>Execution settings</strong>
            {settingsQuery.data ? (
              <div className="meta-grid">
                <span>Jobs</span>
                <input
                  type="number"
                  min={1}
                  max={64}
                  value={settingsQuery.data.concurrency}
                  onChange={(event) => settingsMutation.mutate({ ...settingsQuery.data!, concurrency: Number(event.target.value) })}
                />
                <span>Workspace</span>
                <select
                  value={settingsQuery.data.workspaceMode}
                  onChange={(event) => settingsMutation.mutate({
                    ...settingsQuery.data!,
                    workspaceMode: event.target.value as PiperSettings["workspaceMode"],
                  })}
                >
                  <option value="isolated">isolated</option>
                  <option value="read-only">read-only</option>
                  <option value="writable">writable</option>
                </select>
                <span>Network</span>
                <select
                  value={settingsQuery.data.networkAccess}
                  onChange={(event) => settingsMutation.mutate({
                    ...settingsQuery.data!,
                    networkAccess: event.target.value as PiperSettings["networkAccess"],
                  })}
                >
                  <option value="enabled">enabled</option>
                  <option value="internal">internal</option>
                  <option value="disabled">disabled</option>
                </select>
              </div>
            ) : <p className="muted">Loading…</p>}
          </div>
        </section>

        <section className="logs-panel">
          <div className="logs-heading">
            <span>Live Logs</span>
            {activeRunId ? <strong>{activeRunId}</strong> : <span className="muted">idle</span>}
          </div>
          <LogTerminal
            events={runEvents}
            workflow={selectedWorkflow}
            selectedJobId={selectedJobId}
            onSelectJob={setSelectedJobId}
          />
        </section>
      </main>
    </div>
  );
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

function isBuiltinActionReference(reference: string) {
  return reference.startsWith("actions/checkout@") ||
    reference.startsWith("actions/setup-node@") ||
    reference.startsWith("actions/setup-dotnet@") ||
    reference.startsWith("actions/upload-artifact@") ||
    reference.startsWith("actions/download-artifact@") ||
    reference.startsWith("actions/cache@");
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
