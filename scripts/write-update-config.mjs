import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const apiUrl = process.env.PIPER_UPDATE_API_URL ?? "";
const releasePageUrl = process.env.PIPER_UPDATE_RELEASE_URL ?? "";
if (!apiUrl) {
  throw new Error("PIPER_UPDATE_API_URL is required.");
}

const output = path.resolve(import.meta.dirname, "..", "apps", "desktop", "update-config.json");
fs.writeFileSync(
  output,
  `${JSON.stringify({ provider: "gitlab", apiUrl, releasePageUrl }, null, 2)}\n`,
);
