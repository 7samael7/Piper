import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const platform = process.argv[2] ?? process.platform;
const arch = process.argv[3] ?? process.arch;

const goPlatforms = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};
const goArchitectures = {
  arm64: "arm64",
  x64: "amd64",
};

const goos = goPlatforms[platform];
const goarch = goArchitectures[arch];
if (!goos || !goarch) {
  console.error(`Unsupported desktop target: ${platform}/${arch}.`);
  console.error("Supported platforms are darwin, linux, and win32; supported architectures are arm64 and x64.");
  process.exit(1);
}

const engineFilename = platform === "win32" ? "piper-engine.exe" : "piper-engine";
const engineDirectory = path.join(root, "engine", "bin", `${platform}-${arch}`);
const engineBinary = path.join(engineDirectory, engineFilename);
fs.mkdirSync(engineDirectory, { recursive: true });

run("go", ["build", "-trimpath", "-ldflags=-s -w", "-o", engineBinary, "./cmd/daemon"], {
  cwd: path.join(root, "engine"),
  env: {
    ...process.env,
    CGO_ENABLED: "0",
    GOOS: goos,
    GOARCH: goarch,
  },
});

const npm = process.platform === "win32" ? "npm.cmd" : "npm";
run(
  npm,
  [
    "--workspace",
    "apps/desktop",
    "run",
    "make",
    "--",
    `--platform=${platform}`,
    `--arch=${arch}`,
  ],
  {
    cwd: root,
    env: {
      ...process.env,
      PIPER_ENGINE_BINARY: engineBinary,
    },
  },
);

function run(command, args, options) {
  // Node >=18.20.2/20.12.2/21.7.3 refuses to spawn .cmd/.bat files (e.g. npm.cmd)
  // without shell:true and throws EINVAL. Enable the shell only for those so that
  // commands with spaces in their args (e.g. go's -ldflags) are left untouched.
  const useShell = options?.shell ?? (process.platform === "win32" && /\.(cmd|bat)$/i.test(command));
  const result = spawnSync(command, args, {
    ...options,
    shell: useShell,
    stdio: "inherit",
  });
  if (result.error) {
    console.error(result.error.message);
    process.exit(1);
  }
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}
