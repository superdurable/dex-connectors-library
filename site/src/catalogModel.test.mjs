// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import test from "node:test";

import {
  catalogDocumentUrl,
  capabilityMatchesLanguage,
  companyDirectory,
  companyLogoUrl,
  connectorManifestUrl,
  catalogTotals,
  connectorMatchesQuery,
  connectorMatchesLanguage,
  readCatalog,
  readOperations,
} from "./catalogModel.mjs";

test("company directory is the first folder under connectors", () => {
  assert.equal(companyDirectory("connectors/google/gmail"), "google");
  assert.equal(companyDirectory("connectors/github"), "github");
});

test("manifest URL uses the release tag and repository path", () => {
  assert.equal(
    connectorManifestUrl("connectors/github", "v0.5.0"),
    "https://raw.githubusercontent.com/superdurable/dex-connectors-library/connectors/github/v0.5.0/connectors/github/connector.yaml",
  );
});

test("logo URL uses the company directory", () => {
  assert.equal(companyLogoUrl("google", "local"), "/connectors/google/logo.svg");
  assert.equal(
    companyLogoUrl("google", "published"),
    "https://raw.githubusercontent.com/superdurable/dex-connectors-library/main/connectors/google/logo.svg",
  );
});

test("development reads the local catalog and production reads the site copy", () => {
  assert.equal(catalogDocumentUrl(true, "/"), "/catalog.yaml");
  assert.equal(
    catalogDocumentUrl(false, "/dex-connectors-library/"),
    "/dex-connectors-library/catalog.yaml",
  );
});

test("catalog text exposes company, UI units, operations, and triggers", () => {
  const connectors = readCatalog(`
apiVersion: connectors.dex.dev/catalog/v1alpha1
kind: ConnectorCatalog
connectors:
  - company: Google
    id: gmail
    name: Gmail
    description: Send and read Gmail messages.
    version: v0.8.0
    directory: connectors/google/gmail
    uiUnits:
      - name: searchQueryInput
        description: Enter a Gmail search query.
    triggers:
      - name: messageReceived
        description: Receive a matching message.
    operations:
      - name: sendMessage
        kind: mutation
        description: Send a Gmail message.
`);
  assert.equal(connectors.length, 1);
  assert.equal(connectors[0].company, "Google");
  assert.equal(connectors[0].name, "Gmail");
  assert.equal(connectors[0].description, "Send and read Gmail messages.");
  assert.equal(connectors[0].companyDirectory, "google");
  assert.deepEqual(connectors[0].uiUnits, [
    { name: "searchQueryInput", kind: "ui-unit", description: "Enter a Gmail search query.", languages: ["go"] },
  ]);
  assert.deepEqual(connectors[0].triggers, [
    { name: "messageReceived", kind: "trigger", description: "Receive a matching message.", languages: ["go"] },
  ]);
  assert.deepEqual(connectors[0].operations, [
    { name: "sendMessage", kind: "mutation", description: "Send a Gmail message.", languages: ["go"] },
  ]);
  assert.equal(connectorMatchesQuery(connectors[0], "messageReceived"), true);
  assert.equal(connectorMatchesQuery(connectors[0], "send mutation"), true);
  assert.equal(connectorMatchesQuery(connectors[0], "searchQueryInput"), true);
  assert.equal(connectorMatchesQuery(connectors[0], "spreadsheet"), false);
  assert.deepEqual(catalogTotals(connectors), {
    companies: 1,
    connectors: 1,
    uiUnits: 1,
    triggers: 1,
    operations: 1,
  });
});

test("catalog exposes independent language versions", () => {
  const connectors = readCatalog(`
connectors:
  - company: Google
    id: gmail
    name: Gmail
    description: Mail connector.
    version: v0.2.0
    languages: {go: v0.2.0, python: v0.1.0, typescript: v0.3.0}
    directory: connectors/google/gmail
    uiUnits: []
    triggers: []
    operations: []
`);
  assert.deepEqual(connectors[0].languages, { go: "v0.2.0", python: "v0.1.0", typescript: "v0.3.0" });
  assert.equal(connectorMatchesLanguage(connectors[0], "python"), true);
  assert.equal(connectorMatchesLanguage(connectors[0], "typescript"), true);
  assert.equal(connectorMatchesLanguage(connectors[0], "all"), true);
});

test("catalog reads per-capability support and language filter composes with search", () => {
  const connectors = readCatalog(`
connectors:
  - company: Google
    id: gmail
    name: Gmail
    description: Mail connector.
    version: v0.2.0
    languages: {go: v0.2.0, python: v0.1.0, typescript: v0.3.0}
    directory: connectors/google/gmail
    uiUnits: []
    triggers:
      - {name: messageReceived, description: Receive mail., languages: [go]}
    operations:
      - {name: getMessage, kind: query, description: Read mail., languages: [go, python]}
`);
  assert.deepEqual(connectors[0].triggers[0].languages, ["go"]);
  assert.deepEqual(connectors[0].operations[0].languages, ["go", "python"]);
  assert.equal(connectorMatchesLanguage(connectors[0], "typescript") && connectorMatchesQuery(connectors[0], "mail"), true);
  assert.equal(connectorMatchesLanguage(connectors[0], "python") && connectorMatchesQuery(connectors[0], "stripe"), false);
  assert.equal(connectorMatchesLanguage({ ...connectors[0], languages: { go: "v0.2.0" } }, "python"), false);
  assert.equal(capabilityMatchesLanguage(connectors[0].triggers[0], "typescript"), false);
  assert.equal(capabilityMatchesLanguage(connectors[0].operations[0], "python"), true);
  assert.equal(capabilityMatchesLanguage(connectors[0].triggers[0], "all"), true);
});

test("TypeScript cards omit Go-only Trigger chips while All keeps them", () => {
  const [stripe] = readCatalog(`
connectors:
  - company: Stripe
    id: stripe
    name: Stripe
    description: Payments.
    version: v0.2.0
    languages: {go: v0.2.0, python: v0.1.0, typescript: v0.1.0}
    directory: connectors/stripe
    uiUnits: []
    triggers:
      - {name: checkoutSessionUpdated, description: Receive an update., languages: [go]}
    operations:
      - {name: createCheckoutSession, kind: mutation, description: Create a session., languages: [go, python, typescript]}
`);
  const capabilities = [...stripe.uiUnits, ...stripe.triggers, ...stripe.operations];
  assert.deepEqual(capabilities.filter((capability) => capabilityMatchesLanguage(capability, "all")).map(({ name }) => name),
    ["checkoutSessionUpdated", "createCheckoutSession"]);
  assert.deepEqual(capabilities.filter((capability) => capabilityMatchesLanguage(capability, "typescript")).map(({ name }) => name),
    ["createCheckoutSession"]);
});

test("connector manifest exposes operation name, kind, and description", () => {
  const operations = readOperations(`
spec:
  triggers:
    - name: messageReceived
      description: Receive a matching message.
  operations:
    - name: getMessage
      kind: query
      description: Read one Gmail message.
    - name: sendMessage
      kind: mutation
      description: Send a Gmail message.
`);
  assert.deepEqual(operations, [
    { name: "messageReceived", kind: "trigger", description: "Receive a matching message.", languages: ["go"] },
    { name: "getMessage", kind: "query", description: "Read one Gmail message.", languages: ["go"] },
    { name: "sendMessage", kind: "mutation", description: "Send a Gmail message.", languages: ["go"] },
  ]);
});
