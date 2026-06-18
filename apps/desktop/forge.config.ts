import type { ForgeConfig } from "@electron-forge/shared-types";
import { MakerDeb } from "@electron-forge/maker-deb";
import { MakerDMG } from "@electron-forge/maker-dmg";
import { MakerRpm } from "@electron-forge/maker-rpm";
import { MakerSquirrel } from "@electron-forge/maker-squirrel";
import { MakerZIP } from "@electron-forge/maker-zip";
import { VitePlugin } from "@electron-forge/plugin-vite";
import path from "node:path";

const appIcon = path.resolve(__dirname, "assets/piper-app");

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
    extraResource: ["../../engine/bin/piper-engine", "update-config.json", "assets/piper-app.png"],
    ...(process.env.APPLE_SIGN_IDENTITY ? { osxSign: { identity: process.env.APPLE_SIGN_IDENTITY } } : {}),
    ...(notarize ? { osxNotarize: notarize } : {}),
  },
  rebuildConfig: {},
  makers: [
    new MakerSquirrel({
      setupIcon: `${appIcon}.ico`,
    }),
    new MakerZIP({}, ["darwin"]),
    new MakerDMG({
      format: "ULFO",
      name: "Piper",
    }),
    new MakerRpm({}),
    new MakerDeb({}),
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
