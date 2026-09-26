import { readFileSync, writeFileSync, existsSync } from "node:fs";
import path from "node:path";
import { parseArgs } from "node:util";
import { pathToFileURL } from "node:url";

import { Resvg } from "@resvg/resvg-js";

import { groupCatalog, readCatalog } from "../src/catalogModel.mjs";

const fontCandidates = [
  "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
  "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
  "/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
];

export function companyCountLine(group) {
  const operations = group.connectors.reduce((total, connector) => total + connector.operations.length, 0);
  const triggers = group.connectors.reduce((total, connector) => total + connector.triggers.length, 0);
  return [
    countPhrase(group.connectors.length, "connector", "connectors"),
    countPhrase(operations, "operation", "operations"),
    countPhrase(triggers, "trigger", "triggers"),
  ].join(" · ");
}

function countPhrase(count, singular, plural) {
  return `${count} ${count === 1 ? singular : plural}`;
}

export function renderCatalogCard({ catalogText, repositoryRoot }) {
  const groups = groupCatalog(readCatalog(catalogText));
  const rowHeight = 88;
  const header = 156;
  const height = Math.max(630, header + groups.length * rowHeight + 40);
  const rows = groups
    .map((group, index) => {
      const y = header + index * rowHeight;
      const logoPath = path.join(repositoryRoot, "connectors", group.companyDirectory, "logo.svg");
      const logo = readFileSync(logoPath);
      const href = `data:image/svg+xml;base64,${logo.toString("base64")}`;
      const names = group.connectors.map((connector) => connector.name).join("  ·  ");
      return `
        <image href="${href}" x="56" y="${y}" width="56" height="56"/>
        <text x="132" y="${y + 24}" class="company">${escapeXml(group.company)}</text>
        <text x="1144" y="${y + 24}" class="counts" text-anchor="end">${escapeXml(companyCountLine(group))}</text>
        <text x="132" y="${y + 50}" class="names">${escapeXml(names)}</text>`;
    })
    .join("");
  const svg = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="${height}" viewBox="0 0 1200 ${height}">
  <rect width="1200" height="${height}" fill="#f8fbf5"/>
  <rect width="10" height="${height}" fill="#2e6d3a"/>
  <text x="56" y="78" class="title">Dex Connectors</text>
  <text x="56" y="114" class="subtitle">Published connectors</text>
  ${rows}
  <style>
    .title { font-family: "DejaVu Sans", "Liberation Sans", sans-serif; font-size: 42px; font-weight: 700; fill: #183c25; }
    .subtitle { font-family: "DejaVu Sans", "Liberation Sans", sans-serif; font-size: 20px; fill: #445d48; }
    .company { font-family: "DejaVu Sans", "Liberation Sans", sans-serif; font-size: 24px; font-weight: 700; fill: #183c25; }
    .names { font-family: "DejaVu Sans", "Liberation Sans", sans-serif; font-size: 18px; fill: #445d48; }
    .counts { font-family: "DejaVu Sans", "Liberation Sans", sans-serif; font-size: 18px; fill: #2e6d3a; }
  </style>
</svg>`;
  const fontFiles = fontCandidates.filter((candidate) => existsSync(candidate));
  const renderer = new Resvg(svg, {
    fitTo: { mode: "width", value: 1200 },
    font: { fontFiles, loadSystemFonts: fontFiles.length === 0 },
  });
  return renderer.render().asPng();
}

function escapeXml(value) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function main() {
  const { values } = parseArgs({
    options: {
      catalog: { type: "string" },
      repository: { type: "string" },
      output: { type: "string" },
    },
  });
  if (!values.catalog || !values.repository || !values.output) {
    throw new Error("usage: render-card.mjs --catalog PATH --repository PATH --output PATH");
  }
  const png = renderCatalogCard({
    catalogText: readFileSync(values.catalog, "utf8"),
    repositoryRoot: values.repository,
  });
  writeFileSync(values.output, png);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
