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

/** Logical badge width in CSS pixels, independent of catalog size; the README displays it at this width. */
export const badgeWidth = 900;
/** Logical badge height in CSS pixels, independent of catalog size; the README displays it at this height. */
export const badgeHeight = 36;
/** Device-pixel ratio of the rendered PNG, so the badge stays sharp on high-density displays. */
export const badgePixelScale = 2;

const brandSegmentWidth = 48;
const statSegmentCount = 5;
const statSegmentWidth = (badgeWidth - brandSegmentWidth) / statSegmentCount;
const cornerRadius = 6;
const iconSize = 16;
const iconGap = 8;
const countFontSize = 14;
const labelFontSize = 13;
// Conservative DejaVu Sans advances in em; DejaVu is wider than the fallback fonts.
const boldDigitAdvanceEm = 0.7;
const regularLetterAdvanceEm = 0.62;
const countLabelGap = 5;
const widestCountDigits = 4;
const minimumStatPadding = 8;

// Brand tokens from site/public/favicon.svg and the site's accent green.
const brandBackground = "#00321b";
const brandBarColor = "#f6f8f4";
const brandChevronColor = "#70eea9";
const statBackgrounds = ["#008650", "#007646"];
const statDividerColor = "#00321b";
const countColor = "#ffffff";
const labelColor = "#d3f5e3";
const iconColor = "#b4f5d2";
const fontFamily = `"DejaVu Sans", "Liberation Sans", "Helvetica Neue", Arial, sans-serif`;

// 24 px stroke icons; code, sliders, and bolt match site/src/Icons.tsx.
const statIconShapes = {
  building: `<rect x="4" y="3" width="16" height="18" rx="1.5"/><path d="M8.5 7.5h2M13.5 7.5h2M8.5 11.5h2M13.5 11.5h2M10 21v-4.5h4V21"/>`,
  plug: `<path d="M9 2.5v5M15 2.5v5M6 7.5h12v3.5a6 6 0 0 1-12 0zM12 17v4.5"/>`,
  sliders: `<path d="M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1"/><circle cx="15" cy="6" r="2"/><circle cx="9" cy="12" r="2"/><circle cx="17" cy="18" r="2"/>`,
  code: `<path d="M8 6l-6 6 6 6M16 6l6 6-6 6M14 4l-4 16"/>`,
  bolt: `<path d="M13 2L4 14h7l-1 8 9-12h-7z"/>`,
};

// Same labels and order as the site's CatalogTotals row.
const catalogStatDefinitions = [
  { iconName: "building", label: "companies", totalName: "companies" },
  { iconName: "plug", label: "connectors", totalName: "connectors" },
  { iconName: "sliders", label: "UI units", totalName: "uiUnits" },
  { iconName: "code", label: "operations", totalName: "operations" },
  { iconName: "bolt", label: "triggers", totalName: "triggers" },
];

/**
 * Returns the README badge SVG showing the directory site's catalog totals.
 *
 * The labels and order match the site's CatalogTotals row. The SVG is always
 * badgeWidth by badgeHeight and fits counts of up to four digits.
 */
export function renderCatalogBadgeSvg({ catalogText }) {
  const totals = catalogTotals(readCatalog(catalogText));
  const statSegments = catalogStatDefinitions
    .map(({ iconName, label, totalName }, index) =>
      renderStatSegment({ iconName, label, count: totals[totalName], index }))
    .join("");
  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="${badgeWidth}" height="${badgeHeight}" viewBox="0 0 ${badgeWidth} ${badgeHeight}">
  <clipPath id="badge-shape"><rect width="${badgeWidth}" height="${badgeHeight}" rx="${cornerRadius}"/></clipPath>
  <g clip-path="url(#badge-shape)">
    ${renderBrandSegment()}${statSegments}
  </g>
  <style>
    .icon { fill: none; stroke: ${iconColor}; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; }
    .stat { font-family: ${fontFamily}; }
    .count { font-size: ${countFontSize}px; font-weight: 700; fill: ${countColor}; }
    .label { font-size: ${labelFontSize}px; fill: ${labelColor}; }
  </style>
</svg>`;
}

/** Renders the README badge as a PNG buffer of badgeWidth by badgeHeight times badgePixelScale. */
export function renderCatalogBadgePng({ catalogText }) {
  const fontFiles = fontCandidates.filter((candidate) => existsSync(candidate));
  const renderer = new Resvg(renderCatalogBadgeSvg({ catalogText }), {
    fitTo: { mode: "width", value: badgeWidth * badgePixelScale },
    font: { fontFiles, loadSystemFonts: fontFiles.length === 0 },
  });
  return renderer.render().asPng();
}

function renderBrandSegment() {
  const markSize = 22;
  const markScale = markSize / 64;
  const markLeft = (brandSegmentWidth - markSize) / 2;
  const markTop = (badgeHeight - markSize) / 2;
  return `<rect class="brand" width="${brandSegmentWidth}" height="${badgeHeight}" fill="${brandBackground}"/>
    <g transform="translate(${markLeft} ${markTop}) scale(${markScale})">
      <path d="M 13.5 12 V 52" fill="none" stroke="${brandBarColor}" stroke-width="12" stroke-linecap="round"/>
      <path d="M 30.5 12 L 50.5 32 L 30.5 52" fill="none" stroke="${brandChevronColor}" stroke-width="12" stroke-linecap="round" stroke-linejoin="round"/>
    </g>`;
}

function renderStatSegment({ iconName, label, count, index }) {
  const segmentLeft = brandSegmentWidth + index * statSegmentWidth;
  const countText = String(count);
  const countWidth = countText.length * countFontSize * boldDigitAdvanceEm;
  const labelWidth = label.length * labelFontSize * regularLetterAdvanceEm;
  const contentWidth = iconSize + iconGap + countWidth + countLabelGap + labelWidth;
  const contentLeft = segmentLeft + (statSegmentWidth - contentWidth) / 2;
  const iconTop = (badgeHeight - iconSize) / 2;
  const textBaseline = badgeHeight / 2 + 5;
  const textLeft = contentLeft + iconSize + iconGap;
  const divider = index === 0
    ? ""
    : `<rect x="${segmentLeft}" width="1" height="${badgeHeight}" fill="${statDividerColor}" fill-opacity="0.35"/>`;
  return `
    <g class="stat" data-stat="${label}">
      <rect x="${segmentLeft}" width="${statSegmentWidth}" height="${badgeHeight}" fill="${statBackgrounds[index % 2]}"/>${divider}
      <svg class="icon" data-icon="${iconName}" x="${contentLeft}" y="${iconTop}" width="${iconSize}" height="${iconSize}" viewBox="0 0 24 24">${statIconShapes[iconName]}</svg>
      <text x="${textLeft}" y="${textBaseline}"><tspan class="count">${countText}</tspan><tspan class="label" dx="${countLabelGap}">${label}</tspan></text>
    </g>`;
}

/**
 * Returns whether the widest stat, a four-digit count beside the longest label,
 * fits its fixed segment by conservative DejaVu Sans width estimates.
 */
export function canStatSegmentFitFourDigitCounts() {
  const longestLabelLength = Math.max(...catalogStatDefinitions.map(({ label }) => label.length));
  const widestContentWidth = iconSize + iconGap + widestCountDigits * countFontSize * boldDigitAdvanceEm
    + countLabelGap + longestLabelLength * labelFontSize * regularLetterAdvanceEm;
  return widestContentWidth + 2 * minimumStatPadding <= statSegmentWidth;
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
  const png = renderCatalogBadgePng({ catalogText: readFileSync(values.catalog, "utf8") });
  writeFileSync(values.output, png);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
