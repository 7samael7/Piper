import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@piper/shared-types": path.resolve(__dirname, "../../packages/shared-types/src/index.ts"),
    },
  },
});
