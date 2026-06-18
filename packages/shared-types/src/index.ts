export type ProviderID = "github" | "gitlab" | "azure";

export type SupportLevel =
  | "supported-local"
  | "emulated"
  | "partial"
  | "validation-only"
  | "unsupported"
  | "requires-consent";
export type RuntimeDisposition = "execute" | "emulate" | "inspect-only" | "reject" | "consent";
export type IssueSeverity = "info" | "warning" | "error";
export type RunStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled" | "skipped";

export interface EvaluationError {
  code: string;
  message: string;
  position?: number;
}

export interface ConditionSpec {
  provider: ProviderID;
  original: string;
  kind?: string;
}

export interface ConditionResult {
  expression: string;
  evaluated: boolean;
  value: boolean;
  reason?: string;
  error?: EvaluationError;
}

export interface MatrixSpec {
  dimensions?: Record<string, unknown[]>;
  include?: Record<string, unknown>[];
  exclude?: Record<string, unknown>[];
  azureLegs?: Record<string, Record<string, string>>;
  failFast: boolean;
  maxParallel?: number;
}

export interface ServiceSpec {
  name: string;
  image: string;
  env?: Record<string, string>;
  ports?: string[];
  aliases?: string[];
}

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
  if?: string;
  condition?: ConditionSpec;
  env?: Record<string, string>;
  with?: Record<string, string>;
  continueOnError?: boolean;
  support: SupportLevel;
  featureRefs?: FeatureRef[];
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
  condition?: ConditionSpec;
  steps: PipelineStep[];
  env?: Record<string, string>;
  outputs?: Record<string, string>;
  matrix?: MatrixSpec;
  services?: ServiceSpec[];
  allowFailure?: boolean;
  when?: string;
  environment?: string;
  support: SupportLevel;
  featureRefs?: FeatureRef[];
  unsupportedFeatures?: FeatureSupport[];
}

export interface GraphNode {
  id: string;
  label: string;
  support: SupportLevel;
  logicalJobId?: string;
  matrix?: Record<string, unknown>;
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
  featureId: string;
  feature: string;
  provider: ProviderID | "common";
  category: string;
  path?: string;
  origin?: SourceOrigin;
  support: SupportLevel;
  runtimeDisposition: RuntimeDisposition;
  message: string;
  localBehavior: string;
  hostedDifferences: string;
  securityImplications: string;
  fallback: string;
  documentation: string;
}

export interface SourceOrigin {
  file: string;
  line?: number;
  column?: number;
}

export interface FeatureRef {
  id: string;
  path?: string;
  origin?: SourceOrigin;
}

export interface ValidationReport {
  valid: boolean;
  support: SupportLevel;
  issues: ValidationIssue[];
  features: FeatureSupport[];
}

export interface WorkflowDetails extends WorkflowSummary {
  rawYaml: string;
  resolvedYaml?: string;
  jobs: PipelineJob[];
  graph: {
    nodes: GraphNode[];
    edges: GraphEdge[];
  };
  validation: ValidationReport;
  featureRefs?: FeatureRef[];
  unsupportedFeatures?: FeatureSupport[];
  executionPlan?: {
    jobs: Array<{
      id: string;
      logicalJobId: string;
      name: string;
      needs: string[];
      matrix?: Record<string, unknown>;
    }>;
    maxConcurrency: number;
    maxExpandedJobs: number;
  };
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
  baseRef?: string;
  concurrency?: number;
  maxExpandedJobs?: number;
  workspaceMode?: "writable" | "read-only" | "isolated";
  preparedToken?: string;
  networkAccess?: "enabled" | "disabled" | "internal";
  mockOidc?: boolean;
  mockOidcClaims?: Record<string, string>;
  jobTimeoutSeconds?: number;
  stepTimeoutSeconds?: number;
  memoryMb?: number;
  cpus?: number;
  pidsLimit?: number;
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

export interface ArtifactRecord {
  id: string;
  runId: string;
  jobId: string;
  name: string;
  path: string;
  sourcePaths: string[];
  createdAt: string;
  size: number;
}

export interface CacheRecord {
  id: string;
  scope: string;
  key: string;
  path: string;
  size: number;
  createdAt: string;
  lastUsedAt: string;
}

export interface PiperSettings {
  concurrency: number;
  maxExpandedJobs: number;
  workspaceMode: "writable" | "read-only" | "isolated";
  networkAccess: "enabled" | "disabled" | "internal";
  mockOidc: boolean;
  jobTimeoutSeconds: number;
  stepTimeoutSeconds: number;
  memoryMb: number;
  cpus: number;
  pidsLimit: number;
}

export interface RunPreparation {
  requirements: string[];
  requiresConsent: boolean;
  requiresApproval: boolean;
  preparedToken?: string;
  expiresAt?: string;
}

export interface ProviderCapability {
  featureId: string;
  name: string;
  support: SupportLevel;
  runtimeDisposition: RuntimeDisposition;
}

export interface ProviderInfo {
  id: ProviderID;
  name: string;
  description: string;
  capabilities: ProviderCapability[];
}
