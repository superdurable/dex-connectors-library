// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { parseArgs } from "node:util";
import { pathToFileURL } from "node:url";

import { Resvg } from "@resvg/resvg-js";

import { catalogTotals, readCatalog } from "../src/catalogModel.mjs";

// resvg loads only TTF/OTF/TTC, so the site's Geist woff2 cannot be used here.
const fontCandidates = [
  "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
  "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
  "/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
];

/** Fixed PNG width in pixels, independent of catalog size. */
export const cardWidth = 1200;
/** Fixed PNG height in pixels, independent of catalog size. */
export const cardHeight = 300;

const pagePadding = 48;
const tileGap = 16;
const tileTop = 104;
const tileHeight = 148;
const tileCount = 5;
const tileWidth = (cardWidth - 2 * pagePadding - (tileCount - 1) * tileGap) / tileCount;

// Light-theme tokens from site/src/styles.css.
const siteBackground = "#ffffff";
const siteBackgroundAlt = "#fafaf9";
const siteText = "#111111";
const siteMuted = "#5f5f5f";
const siteFaint = "#8a8a8a";
const siteRule = "#e7e5e4";
const fontFamily = `"DejaVu Sans", "Liberation Sans", "Helvetica Neue", Arial, sans-serif`;

/**
 * Returns the README preview card SVG showing the directory site's catalog totals.
 *
 * The labels and order match the site's CatalogTotals row; the size is always
 * cardWidth by cardHeight.
 */
export function renderCatalogCardSvg({ catalogText }) {
  const totals = catalogTotals(readCatalog(catalogText));
  const totalTiles = [
    ["companies", totals.companies],
    ["connectors", totals.connectors],
    ["UI units", totals.uiUnits],
    ["operations", totals.operations],
    ["triggers", totals.triggers],
  ]
    .map(([label, count], index) => {
      const tileLeft = pagePadding + index * (tileWidth + tileGap);
      return `
  <g transform="translate(${tileLeft} ${tileTop})">
    <rect class="tile" x="0.5" y="0.5" width="${tileWidth - 1}" height="${tileHeight - 1}" rx="12"/>
    <text x="28" y="48" class="label">${label}</text>
    <text x="28" y="116" class="count">${count}</text>
  </g>`;
    })
    .join("");
  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="${cardWidth}" height="${cardHeight}" viewBox="0 0 ${cardWidth} ${cardHeight}">
  <rect width="${cardWidth}" height="${cardHeight}" fill="${siteBackground}"/>
  <text x="${pagePadding}" y="68" class="title">Dex Connectors</text>
  <text x="${cardWidth - pagePadding}" y="68" class="site" text-anchor="end">connectors.superdurable.io</text>
  ${totalTiles}
  <style>
    .title { font-family: ${fontFamily}; font-size: 28px; font-weight: 600; fill: ${siteText}; }
    .site { font-family: ${fontFamily}; font-size: 20px; fill: ${siteFaint}; }
    .tile { fill: ${siteBackgroundAlt}; stroke: ${siteRule}; stroke-width: 1; }
    .label { font-family: ${fontFamily}; font-size: 24px; fill: ${siteMuted}; }
    .count { font-family: ${fontFamily}; font-size: 56px; font-weight: 600; letter-spacing: -1.5px; fill: ${siteText}; }
  </style>
</svg>`;
}

/** Renders the README preview card as a cardWidth by cardHeight PNG buffer. */
export function renderCatalogCard({ catalogText }) {
  const fontFiles = fontCandidates.filter((candidate) => existsSync(candidate));
  const renderer = new Resvg(renderCatalogCardSvg({ catalogText }), {
    fitTo: { mode: "width", value: cardWidth },
    font: { fontFiles, loadSystemFonts: fontFiles.length === 0 },
  });
  return renderer.render().asPng();
}

function main() {
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
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
