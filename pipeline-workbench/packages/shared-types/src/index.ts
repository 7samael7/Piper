export type ProviderID = "github" | "gitlab" | "azure";

export type SupportLevel = "supported" | "partial" | "unsupported";
export type IssueSeverity = "info" | "warning" | "error";
export type RunStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled";

export interface TriggerInput {
  name: string;
  description?: string;
  required?: boolean;
  default?: string;
  type?: string;
}

export interface Trigger {
  name: string;
  inputs?: TriggerInput[];
}

export interface WorkflowSummary {
  id: string;
  provider: ProviderID;
  name: string;
  path: string;
  triggers: Trigger[];
  jobCount: number;
  valid: boolean;
  support: SupportLevel;
}

export interface PipelineStep {
  id?: string;
  name: string;
  uses?: string;
  run?: string;
  shell?: string;
  workingDirectory?: string;
  env?: Record<string, string>;
  with?: Record<string, string>;
  support: SupportLevel;
  unsupportedFeatures?: FeatureSupport[];
}

export interface PipelineJob {
  id: string;
  name: string;
  runner?: string;
  stage?: string;
  image?: string;
  needs: string[];
  if?: string;
  steps: PipelineStep[];
  env?: Record<string, string>;
  support: SupportLevel;
  unsupportedFeatures?: FeatureSupport[];
}

export interface GraphNode {
  id: string;
  label: string;
  support: SupportLevel;
}

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
}

export interface ValidationIssue {
  severity: IssueSeverity;
  code: string;
  message: string;
  path?: string;
  support: SupportLevel;
}

export interface FeatureSupport {
  feature: string;
  path?: string;
  support: SupportLevel;
  message: string;
}

export interface ValidationReport {
  valid: boolean;
  support: SupportLevel;
  issues: ValidationIssue[];
  features: FeatureSupport[];
}

export interface WorkflowDetails extends WorkflowSummary {
  rawYaml: string;
  jobs: PipelineJob[];
  graph: {
    nodes: GraphNode[];
    edges: GraphEdge[];
  };
  validation: ValidationReport;
  unsupportedFeatures?: FeatureSupport[];
}

export interface RunConfig {
  repoPath: string;
  workflowPath: string;
  provider: ProviderID;
  jobId?: string;
  eventName: string;
  inputs: Record<string, string>;
  env: Record<string, string>;
  secrets: Record<string, string>;
}

export interface RunStartResponse {
  runId: string;
}

export interface RunEvent {
  runId: string;
  time: string;
  type: string;
  jobId?: string;
  stepId?: string;
  stream?: "stdout" | "stderr" | "system";
  status?: RunStatus;
  message: string;
  data?: Record<string, unknown>;
}

export interface RunRecord {
  id: string;
  repoPath: string;
  workflowPath: string;
  provider: ProviderID;
  jobId?: string;
  eventName: string;
  status: RunStatus;
  conclusion?: string;
  startedAt: string;
  completedAt?: string;
}

export interface ProviderCapability {
  name: string;
  support: SupportLevel;
}

export interface ProviderInfo {
  id: ProviderID;
  name: string;
  description: string;
  capabilities: ProviderCapability[];
}
