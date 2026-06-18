export type UpdateStatus = "unavailable" | "up-to-date" | "available" | "error";

export interface UpdateCheckResult {
  status: UpdateStatus;
  currentVersion: string;
  latestVersion?: string;
  downloadUrl?: string;
  checksumUrl?: string;
  releaseUrl?: string;
  message?: string;
}
