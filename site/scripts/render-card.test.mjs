import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { connectorCardGridPosition, connectorCountLine, renderCatalogCard } from "./render-card.mjs";

test("catalog card is a PNG", () => {
  const repositoryRoot = mkdtempSync(path.join(tmpdir(), "connector-card-"));
  const logoDirectory = path.join(repositoryRoot, "connectors", "acme");
  mkdirSync(logoDirectory, { recursive: true });
  writeFileSync(
    path.join(logoDirectory, "logo.svg"),
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" fill="#111111"/></svg>`,
  );
  const png = renderCatalogCard({
    repositoryRoot,
    catalogText: `
connectors:
  - company: Acme
    id: mail
    name: Mail
    description: Send mail.
    version: v0.1.0
    directory: connectors/acme/mail
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
`,
  });
  assert.equal(png.subarray(0, 8).toString("hex"), "89504e470d0a1a0a");
  assert.equal(png.readUInt32BE(16), 1200);
  assert.equal(png.readUInt32BE(20), 630);
  assert.equal(connectorCountLine({
    uiUnits: [{ name: "channelPicker" }],
    operations: [{ name: "sendMessage" }],
    triggers: [{ name: "messageReceived" }],
  }), "1 UI · 1 op · 1 trigger");
});

test("catalog card positions connectors in four columns", () => {
  assert.deepEqual(connectorCardGridPosition(0), { x: 56, y: 144 });
  assert.deepEqual(connectorCardGridPosition(3), { x: 884, y: 144 });
  assert.deepEqual(connectorCardGridPosition(4), { x: 56, y: 296 });
  assert.throws(() => connectorCardGridPosition(-1), /non-negative integer/);
});
