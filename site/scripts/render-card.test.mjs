// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import test from "node:test";

import {
  badgeHeight,
  badgePixelScale,
  badgeWidth,
  canStatSegmentFitFourDigitCounts,
  renderCatalogBadgePng,
  renderCatalogBadgeSvg,
} from "./render-card.mjs";

function catalogText(connectorCount) {
  const connectors = Array.from({ length: connectorCount }, (_, index) => `
  - company: Company${index % 7}
    id: mail${index}
    name: Mail ${index}
    description: Send mail.
    version: v0.1.0
    categories: [communication]
    directory: connectors/company${index % 7}/mail${index}
    uiUnits:
      - name: channelPicker
        description: Select one channel.
    triggers:
      - name: messageReceived
        description: Receive a message.
    operations:
      - name: sendMessage
        kind: mutation
        description: Send a message.
      - name: getMessage
        kind: query
        description: Read a message.
`).join("");
  return `categories:
  - {id: communication, name: Communication, description: Chat and email.}
connectors:${connectors}`;
}

function svgDimensions(svg) {
  const match = svg.match(/<svg [^>]*width="(\d+)" height="(\d+)" viewBox="0 0 (\d+) (\d+)"/);
  assert.ok(match, "svg root has width, height, and viewBox");
  return match.slice(1).map(Number);
}

test("catalog badge shows the five catalog totals in site order with icons", () => {
  const svg = renderCatalogBadgeSvg({ catalogText: catalogText(3) });
  const statTexts = [...svg.matchAll(/class="(count|label)"[^>]*>([^<]*)</g)].map((match) => match[2]);
  assert.deepEqual(statTexts, [
    "3", "companies",
    "3", "connectors",
    "3", "UI units",
    "6", "operations",
    "3", "triggers",
  ]);
  const iconNames = [...svg.matchAll(/data-icon="([^"]+)"/g)].map((match) => match[1]);
  assert.deepEqual(iconNames, ["building", "plug", "sliders", "code", "bolt"]);
  assert.doesNotMatch(svg, /Mail 0/);
});

test("catalog badge has no title or site URL text", () => {
  const svg = renderCatalogBadgeSvg({ catalogText: catalogText(3) });
  const visibleTexts = [...svg.matchAll(/<(text|tspan)[^>]*>([^<]+)</g)].map((match) => match[2]);
  for (const visibleText of visibleTexts) {
    assert.doesNotMatch(visibleText, /Dex Connectors|superdurable/);
  }
});

test("catalog badge size does not depend on connector count", () => {
  for (const connectorCount of [1, 500]) {
    const svg = renderCatalogBadgeSvg({ catalogText: catalogText(connectorCount) });
    assert.deepEqual(svgDimensions(svg), [badgeWidth, badgeHeight, badgeWidth, badgeHeight]);
    assert.match(svg, new RegExp(`class="count">${connectorCount * 2}<`));
  }
});

test("catalog badge segments fit four-digit counts", () => {
  assert.ok(canStatSegmentFitFourDigitCounts());
});

test("catalog badge is a fixed-size PNG at the pixel scale", () => {
  const png = renderCatalogBadgePng({ catalogText: catalogText(500) });
  assert.equal(png.subarray(0, 8).toString("hex"), "89504e470d0a1a0a");
  assert.equal(png.readUInt32BE(16), badgeWidth * badgePixelScale);
  assert.equal(png.readUInt32BE(20), badgeHeight * badgePixelScale);
});
