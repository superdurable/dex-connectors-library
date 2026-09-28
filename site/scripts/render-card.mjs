import { readFileSync, writeFileSync, existsSync } from "node:fs";
import path from "node:path";
import { parseArgs } from "node:util";
import { pathToFileURL } from "node:url";

import { Resvg } from "@resvg/resvg-js";

import { readCatalog } from "../src/catalogModel.mjs";

const fontCandidates = [
  "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
  "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
  "/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
];

const cardColumnCount = 4;
const cardWidth = 260;
const cardHeight = 136;
const cardGap = 16;
const gridLeft = 56;
const gridTop = 144;

export function connectorCountLine(connector) {
  return `${connector.uiUnits.length} UI · ${connector.operations.length} ${connector.operations.length === 1 ? "op" : "ops"} · ${connector.triggers.length} ${connector.triggers.length === 1 ? "trigger" : "triggers"}`;
}

export function connectorCardGridPosition(index) {
  if (!Number.isInteger(index) || index < 0) {
    throw new Error("connector card index must be a non-negative integer");
  }
  return {
    x: gridLeft + (index % cardColumnCount) * (cardWidth + cardGap),
    y: gridTop + Math.floor(index / cardColumnCount) * (cardHeight + cardGap),
  };
}

export function renderCatalogCard({ catalogText, repositoryRoot }) {
  const connectors = readCatalog(catalogText);
  const rowCount = Math.ceil(connectors.length / cardColumnCount);
  const gridHeight = rowCount * cardHeight + Math.max(0, rowCount - 1) * cardGap;
  const height = Math.max(630, gridTop + gridHeight + 40);
  const companyCount = new Set(connectors.map((connector) => connector.company)).size;
  const cards = connectors
    .map((connector, index) => {
      const { x, y } = connectorCardGridPosition(index);
      const logoPath = path.join(repositoryRoot, "connectors", connector.companyDirectory, "logo.svg");
      const logo = readFileSync(logoPath);
      const href = `data:image/svg+xml;base64,${logo.toString("base64")}`;
      const titleLines = wrapConnectorTitle(connector.name);
      return `
    <g transform="translate(${x} ${y})">
      <rect class="connector-card" width="${cardWidth}" height="${cardHeight}" rx="14"/>
      <image href="${href}" x="14" y="14" width="36" height="36"/>
      <text x="60" y="29" class="company">${escapeXml(connector.company)}</text>
      <text x="246" y="29" class="version" text-anchor="end">${escapeXml(connector.version)}</text>
      <text x="14" y="76" class="connector-name">${escapeXml(titleLines[0])}</text>
      ${titleLines[1] ? `<text x="14" y="96" class="connector-name">${escapeXml(titleLines[1])}</text>` : ""}
      <text x="14" y="119" class="counts">${escapeXml(connectorCountLine(connector))}</text>
    </g>`;
    })
    .join("");
  const svg = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="${height}" viewBox="0 0 1200 ${height}">
  <rect width="1200" height="${height}" fill="#f8fbf5"/>
  <rect width="10" height="${height}" fill="#2e6d3a"/>
  <text x="56" y="70" class="title">Dex Connectors</text>
  <text x="56" y="108" class="subtitle">${connectors.length} published connectors · ${companyCount} companies</text>
  ${cards}
  <style>
    .title { font-family: "DejaVu Sans", "Liberation Sans", sans-serif; font-size: 42px; font-weight: 700; fill: #183c25; }
    .subtitle { font-family: "DejaVu Sans", "Liberation Sans", sans-serif; font-size: 20px; fill: #445d48; }
    .connector-card { fill: #ffffff; stroke: rgba(55, 103, 55, 0.18); }
    .company { font-family: "DejaVu Sans", "Liberation Sans", sans-serif; font-size: 13px; font-weight: 700; fill: #445d48; }
    .version { font-family: "DejaVu Sans", "Liberation Sans", sans-serif; font-size: 11px; fill: #718373; }
    .connector-name { font-family: "DejaVu Sans", "Liberation Sans", sans-serif; font-size: 17px; font-weight: 700; fill: #183c25; }
    .counts { font-family: "DejaVu Sans", "Liberation Sans", sans-serif; font-size: 12px; fill: #2e6d3a; }
  </style>
</svg>`;
  const fontFiles = fontCandidates.filter((candidate) => existsSync(candidate));
  const renderer = new Resvg(svg, {
    fitTo: { mode: "width", value: 1200 },
    font: { fontFiles, loadSystemFonts: fontFiles.length === 0 },
  });
  return renderer.render().asPng();
}

function wrapConnectorTitle(value) {
  const words = value.split(/\s+/);
  let firstLine = "";
  let secondLine = "";
  for (const word of words) {
    const firstLineCandidate = firstLine ? `${firstLine} ${word}` : word;
    if (!secondLine && (!firstLine || firstLineCandidate.length <= 28)) {
      firstLine = firstLineCandidate;
      continue;
    }
    secondLine = secondLine ? `${secondLine} ${word}` : word;
  }
  if (secondLine.length > 28) {
    secondLine = `${secondLine.slice(0, 27).trimEnd()}…`;
  }
  return secondLine ? [firstLine, secondLine] : [firstLine];
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
