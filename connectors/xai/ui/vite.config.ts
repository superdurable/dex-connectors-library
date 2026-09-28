// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { viteSingleFile } from "vite-plugin-singlefile";

export default defineConfig({
  plugins: [react(), viteSingleFile()],
  // sdk/react is linked with its own React; one copy keeps its hooks working.
  resolve: { dedupe: ["react", "react-dom"] },
  build: { target: "es2022", assetsInlineLimit: 100_000_000 },
});
