import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { Resvg } from "@resvg/resvg-js";
import { parseArgs } from "node:util";
import { groupCatalog, readCatalog } from "../src/catalogModel.mjs";

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

export function renderCatalogCard({ catalogText }) {
  const groups = groupCatalog(readCatalog(catalogText));
  const rows = groups.map((group, index) => {
    const y = 168 + index * 72;
    const names = group.connectors.map((connector) => connector.name).join(" · ");
    return `<text x="72" y="${y}" class="company">${escapeXml(group.company)}</text>
      <text x="72" y="${y + 28}" class="names">${escapeXml(names)}</text>
      <text x="1144" y="${y + 8}" class="counts" text-anchor="end">${escapeXml(companyCountLine(group))}</text>`;
  }).join("\n");
  const height = 140 + groups.length * 72 + 36;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="${height}" viewBox="0 0 1200 ${height}">
    <rect width="1200" height="${height}" fill="#f8fbf5"/>
    <rect width="12" height="${height}" fill="#2e6d3a"/>
    <text x="72" y="72" class="title">Dex Connectors</text>
    <text x="72" y="108" class="subtitle">Published connectors</text>
    ${rows}
    <style>
      .title { font: 600 40px sans-serif; fill: #183c25; }
      .subtitle, .names { font: 18px sans-serif; fill: #718373; }
      .company { font: 600 24px sans-serif; fill: #183c25; }
      .counts { font: 18px sans-serif; fill: #2e6d3a; }
    </style>
  </svg>`;
  return new Resvg(svg, { fitTo: { mode: "width", value: 1200 } }).render().asPng();
}

function escapeXml(value) {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const { values } = parseArgs({
    options: {
      catalog: { type: "string" },
      output: { type: "string" },
    },
  });
  if (!values.catalog || !values.output) {
    throw new Error("usage: render-card.mjs --catalog PATH --output PATH");
  }
  const png = renderCatalogCard({ catalogText: readFileSync(values.catalog, "utf8") });
  writeFileSync(values.output, png);
  process.stdout.write(`${path.resolve(values.output)}\n`);
}
