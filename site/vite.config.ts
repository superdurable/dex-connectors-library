import { createReadStream, existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";

const siteRoot = path.dirname(fileURLToPath(import.meta.url));
const repositoryRoot = path.resolve(siteRoot, "..");
const localCatalogPath = path.join(siteRoot, ".catalog.yaml");

function serveLocalCatalog(): Plugin {
  return {
    name: "serve-local-catalog",
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        const url = request.url?.split("?")[0] ?? "";
        if (url !== "/catalog.yaml") {
          next();
          return;
        }
        if (!existsSync(localCatalogPath)) {
          response.statusCode = 404;
          response.end("local catalog is missing; run npm run dev");
          return;
        }
        response.setHeader("content-type", "text/yaml");
        response.end(readFileSync(localCatalogPath));
      });
    },
  };
}

function serveCompanyLogos(): Plugin {
  return {
    name: "serve-company-logos",
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        const url = request.url?.split("?")[0] ?? "";
        const match = url.match(/^\/connectors\/([^/]+)\/logo\.svg$/);
        if (!match || match[1] === "." || match[1] === "..") {
          next();
          return;
        }
        const logoPath = path.resolve(repositoryRoot, "connectors", match[1], "logo.svg");
        const connectorsRoot = path.resolve(repositoryRoot, "connectors") + path.sep;
        if (!logoPath.startsWith(connectorsRoot) || !existsSync(logoPath)) {
          response.statusCode = 404;
          response.end("logo not found");
          return;
        }
        response.setHeader("content-type", "image/svg+xml");
        createReadStream(logoPath).pipe(response);
      });
    },
  };
}

export default defineConfig(({ command }) => ({
  base: command === "serve" ? "/" : "/dex-connectors-library/",
  plugins: [react(), serveLocalCatalog(), serveCompanyLogos()],
  build: { outDir: "dist", emptyOutDir: true },
}));
