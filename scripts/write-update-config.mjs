import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const repository = process.env.PIPER_UPDATE_REPOSITORY ?? process.env.GITHUB_REPOSITORY ?? "";
if (!/^[^/\s]+\/[^/\s]+$/.test(repository)) {
  throw new Error("PIPER_UPDATE_REPOSITORY must be in owner/repository format.");
}

const output = path.resolve(import.meta.dirname, "..", "apps", "desktop", "update-config.json");
fs.writeFileSync(
  output,
  `${JSON.stringify(
    {
      provider: "github",
      apiUrl: `https://api.github.com/repos/${repository}/releases/latest`,
      releasePageUrl: `https://github.com/${repository}/releases/latest`,
    },
    null,
    2,
  )}\n`,
);
