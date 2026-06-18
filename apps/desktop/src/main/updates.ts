import { app, net, shell } from "electron";
import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import type { UpdateCheckResult } from "../shared/updates";

interface UpdateConfig {
  provider: "gitlab";
  apiUrl: string;
  releasePageUrl?: string;
}

interface GitLabReleaseLink {
  name: string;
  url: string;
  direct_asset_url?: string;
}

interface GitLabRelease {
  tag_name: string;
  _links?: {
    self?: string;
  };
  assets?: {
    links?: GitLabReleaseLink[];
  };
}

export class UpdateService {
  private latestResult?: UpdateCheckResult;

  async check(): Promise<UpdateCheckResult> {
    const currentVersion = app.getVersion();
    if (process.platform !== "darwin") {
      return this.remember({
        status: "unavailable",
        currentVersion,
        message: "DMG update checks are available on macOS.",
      });
    }
    const config = loadUpdateConfig();
    if (!config?.apiUrl) {
      return this.remember({
        status: "unavailable",
        currentVersion,
        message: "Update checks are not configured for this build.",
      });
    }

    try {
      const apiUrl = safeExternalUrl(config.apiUrl);
      const headers: Record<string, string> = { Accept: "application/json" };
      if (process.env.PIPER_UPDATE_TOKEN) {
        headers["PRIVATE-TOKEN"] = process.env.PIPER_UPDATE_TOKEN;
      }

      const response = await net.fetch(apiUrl, { headers });
      if (response.status === 404) {
        return this.remember({
          status: "up-to-date",
          currentVersion,
          message: "No published release is newer than this build.",
        });
      }
      if (!response.ok) {
        throw new Error(`GitLab returned HTTP ${response.status}.`);
      }

      const release = (await response.json()) as GitLabRelease;
      const latestVersion = normalizeVersion(release.tag_name);
      const comparison = compareVersions(latestVersion, currentVersion);
      const releaseUrl = optionalSafeExternalUrl(config.releasePageUrl ?? release._links?.self);
      if (comparison <= 0) {
        return this.remember({
          status: "up-to-date",
          currentVersion,
          latestVersion,
          releaseUrl,
          message: `Piper ${currentVersion} is current.`,
        });
      }

      const selectedAsset = selectDmgAsset(release.assets?.links ?? []);
      return this.remember({
        status: "available",
        currentVersion,
        latestVersion,
        downloadUrl: selectedAsset?.downloadUrl,
        checksumUrl: selectedAsset?.checksumUrl,
        releaseUrl,
        message: `Piper ${latestVersion} is available.`,
      });
    } catch (error) {
      return this.remember({
        status: "error",
        currentVersion,
        message: error instanceof Error ? error.message : String(error),
      });
    }
  }

  async openLatest(): Promise<void> {
    const result = this.latestResult?.status === "available" ? this.latestResult : await this.check();
    if (!result.downloadUrl && result.releaseUrl) {
      await shell.openExternal(safeExternalUrl(result.releaseUrl));
      return;
    }
    if (!result.downloadUrl || !result.latestVersion) {
      throw new Error("The latest release does not contain a downloadable macOS installer.");
    }

    const filename = `Piper-${result.latestVersion}-${process.arch}.dmg`;
    const destination = path.join(app.getPath("downloads"), filename);
    const temporaryPath = `${destination}.download`;
    const headers = updateRequestHeaders();
    try {
      await downloadFile(result.downloadUrl, temporaryPath, headers);
      if (result.checksumUrl) {
        const checksumResponse = await net.fetch(safeExternalUrl(result.checksumUrl), { headers });
        if (!checksumResponse.ok) {
          throw new Error(`Could not download the installer checksum (HTTP ${checksumResponse.status}).`);
        }
        const expectedChecksum = (await checksumResponse.text()).match(/\b[0-9a-f]{64}\b/i)?.[0]?.toLowerCase();
        if (!expectedChecksum) {
          throw new Error("The release checksum is invalid.");
        }
        const actualChecksum = await sha256(temporaryPath);
        if (actualChecksum !== expectedChecksum) {
          throw new Error("The downloaded installer failed SHA-256 verification.");
        }
      }
      fs.renameSync(temporaryPath, destination);
    } finally {
      fs.rmSync(temporaryPath, { force: true });
    }

    const openError = await shell.openPath(destination);
    if (openError) {
      throw new Error(openError);
    }
  }

  private remember(result: UpdateCheckResult) {
    this.latestResult = result;
    return result;
  }
}

export function compareVersions(left: string, right: string): number {
  const a = parseVersion(left);
  const b = parseVersion(right);
  for (let index = 0; index < Math.max(a.numbers.length, b.numbers.length); index += 1) {
    const difference = (a.numbers[index] ?? 0) - (b.numbers[index] ?? 0);
    if (difference !== 0) {
      return difference > 0 ? 1 : -1;
    }
  }
  if (a.prerelease === b.prerelease) {
    return 0;
  }
  if (!a.prerelease) {
    return 1;
  }
  if (!b.prerelease) {
    return -1;
  }
  return a.prerelease.localeCompare(b.prerelease, undefined, { numeric: true });
}

function loadUpdateConfig(): UpdateConfig | undefined {
  const configPath = app.isPackaged
    ? path.join(process.resourcesPath, "update-config.json")
    : path.join(app.getAppPath(), "update-config.json");
  try {
    return JSON.parse(fs.readFileSync(configPath, "utf8")) as UpdateConfig;
  } catch {
    return undefined;
  }
}

function selectDmgAsset(links: GitLabReleaseLink[]): { downloadUrl: string; checksumUrl?: string } | undefined {
  const dmgLinks = links.filter((link) => link.name.toLowerCase().endsWith(".dmg"));
  const architectureNames = process.arch === "arm64" ? ["arm64", "aarch64"] : ["x64", "amd64", "x86_64"];
  const selected =
    dmgLinks.find((link) => architectureNames.some((architecture) => link.name.toLowerCase().includes(architecture))) ??
    (dmgLinks.length === 1 ? dmgLinks[0] : undefined);
  if (!selected) {
    return undefined;
  }
  const checksum = links.find((link) => link.name === `${selected.name}.sha256`);
  return {
    downloadUrl: safeExternalUrl(selected.direct_asset_url ?? selected.url),
    checksumUrl: optionalSafeExternalUrl(checksum?.direct_asset_url ?? checksum?.url),
  };
}

function normalizeVersion(value: string): string {
  const normalized = value.trim().replace(/^v/i, "");
  parseVersion(normalized);
  return normalized;
}

function parseVersion(value: string) {
  const match = value.trim().replace(/^v/i, "").match(/^(\d+(?:\.\d+)*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/);
  if (!match) {
    throw new Error(`Release version ${JSON.stringify(value)} is not semantic versioning compatible.`);
  }
  return {
    numbers: match[1].split(".").map(Number),
    prerelease: match[2] ?? "",
  };
}

function optionalSafeExternalUrl(value?: string): string | undefined {
  if (!value) {
    return undefined;
  }
  return safeExternalUrl(value);
}

function safeExternalUrl(value: string): string {
  const url = new URL(value);
  if (url.protocol !== "https:" && !(url.protocol === "http:" && ["localhost", "127.0.0.1", "::1"].includes(url.hostname))) {
    throw new Error("Update URLs must use HTTPS.");
  }
  return url.toString();
}

function updateRequestHeaders(): Record<string, string> {
  const headers: Record<string, string> = {};
  if (process.env.PIPER_UPDATE_TOKEN) {
    headers["PRIVATE-TOKEN"] = process.env.PIPER_UPDATE_TOKEN;
  }
  return headers;
}

async function downloadFile(url: string, destination: string, headers: Record<string, string>): Promise<void> {
  const response = await net.fetch(safeExternalUrl(url), { headers });
  if (!response.ok || !response.body) {
    throw new Error(`Could not download the installer (HTTP ${response.status}).`);
  }
  const source = Readable.fromWeb(response.body as unknown as import("node:stream/web").ReadableStream);
  await pipeline(source, fs.createWriteStream(destination));
}

async function sha256(filename: string): Promise<string> {
  const hash = createHash("sha256");
  for await (const chunk of fs.createReadStream(filename)) {
    hash.update(chunk);
  }
  return hash.digest("hex");
}
