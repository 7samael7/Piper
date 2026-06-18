import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const version = process.argv[2]?.replace(/^v/, "");
if (!version || !/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) {
  throw new Error("Usage: node scripts/set-version.mjs <semantic-version>");
}

const root = path.resolve(import.meta.dirname, "..");
const manifests = [
  path.join(root, "package.json"),
  path.join(root, "apps", "desktop", "package.json"),
  path.join(root, "packages", "shared-types", "package.json"),
];

for (const filename of manifests) {
  const manifest = JSON.parse(fs.readFileSync(filename, "utf8"));
  manifest.version = version;
  if (manifest.dependencies?.["@piper/shared-types"]) {
    manifest.dependencies["@piper/shared-types"] = version;
  }
  fs.writeFileSync(filename, `${JSON.stringify(manifest, null, 2)}\n`);
}
