import path from "node:path";
import { readFile } from "node:fs/promises";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import type { Plugin } from "vite";

const siteRoot = path.dirname(new URL(import.meta.url).pathname);
const repositoryRoot = path.resolve(siteRoot, "..");

export default defineConfig(({ command }) => ({
  base: command === "serve" ? "/" : "/dex-connectors-library/",
  plugins: [react(), serveLocalCatalog(), serveCompanyLogos()],
}));

function serveLocalCatalog(): Plugin {
  return {
    name: "serve-local-catalog",
    configureServer(server) {
      server.middlewares.use(async (request, response, next) => {
        const url = request.url?.split("?")[0];
        if (url !== "/catalog.yaml") {
          next();
          return;
        }
        const catalog = await readFile(path.join(siteRoot, ".catalog.yaml"));
        response.setHeader("Content-Type", "application/yaml");
        response.end(catalog);
      });
    },
  };
}

function serveCompanyLogos(): Plugin {
  return {
    name: "serve-company-logos",
    configureServer(server) {
      server.middlewares.use(async (request, response, next) => {
        const url = request.url?.split("?")[0] ?? "";
        const match = url.match(/^\/connectors\/([a-z0-9]+)\/logo\.svg$/);
        if (!match) {
          next();
          return;
        }
        const logoPath = path.resolve(repositoryRoot, "connectors", match[1], "logo.svg");
        const connectorsRoot = path.resolve(repositoryRoot, "connectors") + path.sep;
        if (!logoPath.startsWith(connectorsRoot)) {
          next();
          return;
        }
        const logo = await readFile(logoPath);
        response.setHeader("Content-Type", "image/svg+xml");
        response.end(logo);
      });
    },
  };
}
