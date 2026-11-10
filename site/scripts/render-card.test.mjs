// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import test from "node:test";

import { cardHeight, cardWidth, renderCatalogCard, renderCatalogCardSvg } from "./render-card.mjs";

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

test("catalog card shows the five catalog totals in site order", () => {
  const svg = renderCatalogCardSvg({ catalogText: catalogText(3) });
  const tileTexts = [...svg.matchAll(/class="(label|count)">([^<]*)</g)].map((match) => match[2]);
  assert.deepEqual(tileTexts, [
    "companies", "3",
    "connectors", "3",
    "UI units", "3",
    "operations", "6",
    "triggers", "3",
  ]);
  assert.doesNotMatch(svg, /Mail 0/);
});

test("catalog card size does not depend on connector count", () => {
  for (const connectorCount of [1, 500]) {
    const svg = renderCatalogCardSvg({ catalogText: catalogText(connectorCount) });
    assert.deepEqual(svgDimensions(svg), [cardWidth, cardHeight, cardWidth, cardHeight]);
    assert.match(svg, new RegExp(`class="count">${connectorCount * 2}<`));
  }
});

test("catalog card is a fixed-size PNG", () => {
  const png = renderCatalogCard({ catalogText: catalogText(500) });
  assert.equal(png.subarray(0, 8).toString("hex"), "89504e470d0a1a0a");
  assert.equal(png.readUInt32BE(16), cardWidth);
  assert.equal(png.readUInt32BE(20), cardHeight);
});
