import type { ForgeConfig } from "@electron-forge/shared-types";
import { MakerDeb } from "@electron-forge/maker-deb";
import { MakerDMG } from "@electron-forge/maker-dmg";
import { MakerRpm } from "@electron-forge/maker-rpm";
import { MakerSquirrel } from "@electron-forge/maker-squirrel";
import { MakerZIP } from "@electron-forge/maker-zip";
import { VitePlugin } from "@electron-forge/plugin-vite";
import path from "node:path";

const appIcon = path.resolve(__dirname, "assets/piper-app");
const appIconPng = `${appIcon}.png`;
const engineBinary = process.env.PIPER_ENGINE_BINARY
  ? path.resolve(process.env.PIPER_ENGINE_BINARY)
  : path.resolve(__dirname, `../../engine/bin/${process.platform === "win32" ? "piper-engine.exe" : "piper-engine"}`);
const linuxPackageOptions = {
  name: "piper",
  bin: "Piper",
  productName: "Piper",
  genericName: "CI/CD Workbench",
  description: "Local CI/CD workbench desktop app",
  productDescription: "Inspect, validate, visualize, and run CI/CD pipelines locally.",
  homepage: "https://github.com/7samael7/Piper",
  icon: appIconPng,
};

const notarize =
  process.env.APPLE_ID && process.env.APPLE_APP_SPECIFIC_PASSWORD && process.env.APPLE_TEAM_ID
    ? {
        appleId: process.env.APPLE_ID,
        appleIdPassword: process.env.APPLE_APP_SPECIFIC_PASSWORD,
        teamId: process.env.APPLE_TEAM_ID,
      }
    : undefined;

const config: ForgeConfig = {
  packagerConfig: {
    asar: true,
    appBundleId: "dev.piper.desktop",
    icon: appIcon,
    extraResource: [engineBinary, "update-config.json", appIconPng],
    ...(process.env.APPLE_SIGN_IDENTITY ? { osxSign: { identity: process.env.APPLE_SIGN_IDENTITY } } : {}),
    ...(notarize ? { osxNotarize: notarize } : {}),
  },
  rebuildConfig: {},
  makers: [
    new MakerSquirrel({
      name: "Piper",
      authors: "Piper contributors",
      setupExe: "Piper Setup.exe",
      setupIcon: `${appIcon}.ico`,
    }),
    new MakerZIP({}, ["darwin"]),
    new MakerDMG({
      format: "ULFO",
      name: "Piper",
    }),
    new MakerRpm({
      options: {
        ...linuxPackageOptions,
        categories: ["Development"],
        license: "Proprietary",
      },
    }),
    new MakerDeb({
      options: {
        ...linuxPackageOptions,
        categories: ["Development"],
        maintainer: "Piper contributors",
      },
    }),
  ],
  plugins: [
    new VitePlugin({
      build: [
        {
          entry: "src/main/index.ts",
          config: "vite.main.config.ts",
          target: "main",
        },
        {
          entry: "src/preload/index.ts",
          config: "vite.preload.config.ts",
          target: "preload",
        },
      ],
      renderer: [
        {
          name: "main_window",
          config: "vite.config.ts",
        },
      ],
    }),
  ],
};

export default config;
