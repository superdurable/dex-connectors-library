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
  pythonInstall,
  readCatalog,
  readCatalogDocument,
  readOperations,
  allCategories,
  categoryConnectorCounts,
  languageConnectorCounts,
  connectorMatchesCategory,
  selectedCategoryID,
} from "./catalogModel.mjs";

const categoryTaxonomy = `categories:
  - {id: ai, name: AI & Machine Learning, description: Models.}
  - {id: communication, name: Communication, description: Chat and email.}
  - {id: finance-payments, name: "Finance, Payments & Accounting", description: Payments.}
  - {id: hr-recruiting, name: HR & Recruiting, description: People.}
`;

function categorizedCatalog() {
  return readCatalogDocument(`${categoryTaxonomy}connectors:
  - {company: Google, id: gmail, name: Gmail, description: Send mail., version: v0.1.0, categories: [communication],
     directory: connectors/google/gmail, uiUnits: [], triggers: [], operations: [{name: sendMessage, kind: mutation, description: Send.}]}
  - {company: OpenAI, id: openai, name: OpenAI, description: Generate text., version: v0.1.0, categories: [ai],
     directory: connectors/openai, uiUnits: [], triggers: [], operations: []}
  - {company: Slack, id: slack, name: Slack, description: Post messages with an assistant., version: v0.1.0,
     categories: [communication, ai], languages: {go: v0.1.0, python: v0.1.0},
     directory: connectors/slack, uiUnits: [], triggers: [], operations: []}
  - {company: Stripe, id: stripe, name: Stripe, description: Take payments., version: v0.1.0, categories: [finance-payments],
     directory: connectors/stripe, uiUnits: [], triggers: [], operations: []}
`);
}

test("catalog reads the category taxonomy in order and resolves connector categories, primary first", () => {
  const { categories, connectors } = categorizedCatalog();
  assert.deepEqual(categories.map(({ id }) => id), ["ai", "communication", "finance-payments", "hr-recruiting"]);
  assert.equal(categories[2].name, "Finance, Payments & Accounting");
  assert.equal(new Set(categories.map(({ hue }) => hue)).size, categories.length);
  const slack = connectors.find(({ id }) => id === "slack");
  assert.deepEqual(slack.categories.map(({ id }) => id), ["communication", "ai"]);
  assert.equal(slack.categories[0].name, "Communication");
});

test("category filter matches primary and secondary categories", () => {
  const { connectors } = categorizedCatalog();
  const matching = (categoryID) => connectors.filter((connector) => connectorMatchesCategory(connector, categoryID))
    .map(({ id }) => id);
  assert.deepEqual(matching("ai"), ["openai", "slack"]);
  assert.deepEqual(matching("communication"), ["gmail", "slack"]);
  assert.deepEqual(matching("hr-recruiting"), []);
  assert.deepEqual(matching(allCategories), ["gmail", "openai", "slack", "stripe"]);
});

test("category counts include every category, count secondary membership, and follow other filters", () => {
  const { categories, connectors } = categorizedCatalog();
  assert.deepEqual([...categoryConnectorCounts(categories, connectors)], [
    ["all", 4], ["ai", 2], ["communication", 2], ["finance-payments", 1], ["hr-recruiting", 0],
  ]);
  const pythonMatches = connectors.filter((connector) => connectorMatchesLanguage(connector, "python"));
  assert.deepEqual([...categoryConnectorCounts(categories, pythonMatches)], [
    ["all", 1], ["ai", 1], ["communication", 1], ["finance-payments", 0], ["hr-recruiting", 0],
  ]);
  assert.deepEqual([...languageConnectorCounts(connectors)], [["all", 4], ["go", 4], ["python", 1], ["typescript", 0]]);
  const aiMatches = connectors.filter((connector) => connectorMatchesCategory(connector, "ai"));
  assert.deepEqual([...languageConnectorCounts(aiMatches)], [["all", 2], ["go", 2], ["python", 1], ["typescript", 0]]);
});

test("category filter composes with search, and search matches category names", () => {
  const { connectors } = categorizedCatalog();
  const matching = (categoryID, query) => connectors
    .filter((connector) => connectorMatchesCategory(connector, categoryID) && connectorMatchesQuery(connector, query))
    .map(({ id }) => id);
  assert.deepEqual(matching("communication", "assistant"), ["slack"]);
  assert.deepEqual(matching("communication", "send"), ["gmail"]);
  assert.deepEqual(matching("ai", "payments"), []);
  assert.deepEqual(matching(allCategories, "machine learning"), ["openai", "slack"]);
});

test("URL category falls back to all when absent or unknown", () => {
  const { categories } = categorizedCatalog();
  assert.equal(selectedCategoryID("ai", categories), "ai");
  assert.equal(selectedCategoryID(null, categories), allCategories);
  assert.equal(selectedCategoryID("spreadsheets", categories), allCategories);
});

test("catalog without categories degrades to uncategorized connectors", () => {
  const connector = (categories) => `
  - {company: Google, id: gmail, name: Gmail, description: Mail., version: v0.1.0${categories},
     directory: connectors/google/gmail, uiUnits: [], triggers: [], operations: []}`;
  const legacy = readCatalogDocument(`connectors:${connector("")}`);
  assert.deepEqual(legacy.categories, []);
  assert.deepEqual(legacy.connectors[0].categories, []);
  assert.deepEqual([...categoryConnectorCounts(legacy.categories, legacy.connectors)], [[allCategories, 1]]);
  assert.equal(connectorMatchesCategory(legacy.connectors[0], allCategories), true);
  assert.deepEqual(readCatalog(`${categoryTaxonomy}connectors:${connector("")}`)[0].categories, []);
  const resolved = readCatalog(`${categoryTaxonomy}connectors:${connector(", categories: [spreadsheets, ai, ai]")}`);
  assert.deepEqual(resolved[0].categories.map(({ id }) => id), ["ai"]);
});

test("catalog rejects a malformed category taxonomy", () => {
  const connector = `
  - {company: Google, id: gmail, name: Gmail, description: Mail., version: v0.1.0, categories: [ai],
     directory: connectors/google/gmail, uiUnits: [], triggers: [], operations: []}`;
  assert.throws(() => readCatalog(`categories: ai\nconnectors:${connector}`), /must be a list/);
  assert.throws(() => readCatalog(`categories: [{id: ai, name: AI, description: x}, {id: ai, name: AI, description: x}]
connectors:${connector}`), /invalid or repeated/);
});

test("company directory is the first folder under connectors", () => {
  assert.equal(companyDirectory("connectors/google/gmail"), "google");
  assert.equal(companyDirectory("connectors/github"), "github");
});

test("manifest URL uses the release tag and repository path", () => {
  assert.equal(
    connectorManifestUrl("connectors/github", "v0.5.0"),
    "https://raw.githubusercontent.com/superdurable/dex-connectors-library/connectors/github/go/v0.5.0/connectors/github/connector.yaml",
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
  assert.equal(catalogDocumentUrl(false, "./"), "./catalog.yaml");
});

test("catalog text exposes company, UI units, operations, and triggers", () => {
  const connectors = readCatalog(`
apiVersion: connectors.dex.dev/catalog/v1alpha1
kind: ConnectorCatalog
${categoryTaxonomy}connectors:
  - company: Google
    id: gmail
    name: Gmail
    description: Send and read Gmail messages.
    version: v0.8.0
    categories: [communication]
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
${categoryTaxonomy}connectors:
  - company: Google
    id: gmail
    name: Gmail
    description: Mail connector.
    version: v0.2.0
    categories: [communication]
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

function pythonCatalog(pythonPackage) {
  return `
${categoryTaxonomy}connectors:
  - company: Calendly
    id: calendly
    name: Calendly
    description: Scheduling connector.
    version: v0.2.0
    categories: [communication]
    languages: {go: v0.2.0, python: v0.1.0}
${pythonPackage}
    directory: connectors/calendly
    uiUnits: []
    triggers: []
    operations: []
`;
}

test("Python install uses PyPI only after the upload is published", () => {
  const [published] = readCatalog(pythonCatalog(
    "    pythonPackage: {name: dex-connector-calendly, tag: connectors/calendly/python/v0.1.0, pypiPublished: true}"));
  assert.deepEqual(pythonInstall(published), {
    isPyPIPublished: true,
    command: "pip install dex-connector-calendly==0.1.0",
  });
  const [pending] = readCatalog(pythonCatalog(
    "    pythonPackage: {name: dex-connector-calendly, tag: connectors/calendly/python/v0.1.0, pypiPublished: false}"));
  assert.deepEqual(pythonInstall(pending), {
    isPyPIPublished: false,
    command: 'pip install "dex-connector-calendly @ git+https://github.com/superdurable/dex-connectors-library' +
      '@connectors/calendly/python/v0.1.0#subdirectory=connectors/calendly/python"',
    releaseUrl: "https://github.com/superdurable/dex-connectors-library/releases/tag/connectors/calendly/python/v0.1.0",
  });
  const [unknown] = readCatalog(pythonCatalog(""));
  assert.equal(unknown.pythonPackage, null);
  assert.equal(pythonInstall(unknown), null);
});

test("catalog rejects a Python package that differs from the listed Python release", () => {
  assert.throws(() => readCatalog(pythonCatalog(
    "    pythonPackage: {name: dex-connector-calendly, tag: connectors/calendly/python/v0.0.9, pypiPublished: true}")),
  /pythonPackage/);
  assert.throws(() => readCatalog(pythonCatalog(
    "    pythonPackage: {name: dex-connector-calendly, tag: connectors/calendly/python/v0.1.0, pypiPublished: maybe}")),
  /pythonPackage/);
});

test("catalog reads per-capability support and language filter composes with search", () => {
  const connectors = readCatalog(`
${categoryTaxonomy}connectors:
  - company: Google
    id: gmail
    name: Gmail
    description: Mail connector.
    version: v0.2.0
    categories: [communication]
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
${categoryTaxonomy}connectors:
  - company: Stripe
    id: stripe
    name: Stripe
    description: Payments.
    version: v0.2.0
    categories: [communication]
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
