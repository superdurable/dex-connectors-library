import assert from "node:assert/strict";
import test from "node:test";

import {
  catalogDocumentUrl,
  companyDirectory,
  companyLogoUrl,
  connectorManifestUrl,
  catalogTotals,
  connectorMatchesQuery,
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

test("catalog text exposes company, operations, and triggers", () => {
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
  assert.deepEqual(connectors[0].triggers, [
    { name: "messageReceived", kind: "trigger", description: "Receive a matching message." },
  ]);
  assert.deepEqual(connectors[0].operations, [
    { name: "sendMessage", kind: "mutation", description: "Send a Gmail message." },
  ]);
  assert.equal(connectorMatchesQuery(connectors[0], "messageReceived"), true);
  assert.equal(connectorMatchesQuery(connectors[0], "send mutation"), true);
  assert.equal(connectorMatchesQuery(connectors[0], "spreadsheet"), false);
  assert.deepEqual(catalogTotals(connectors), {
    companies: 1,
    connectors: 1,
    triggers: 1,
    operations: 1,
  });
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
    { name: "messageReceived", kind: "trigger", description: "Receive a matching message." },
    { name: "getMessage", kind: "query", description: "Read one Gmail message." },
    { name: "sendMessage", kind: "mutation", description: "Send a Gmail message." },
  ]);
});
