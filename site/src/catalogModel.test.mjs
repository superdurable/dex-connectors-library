import assert from "node:assert/strict";
import test from "node:test";
import {
  catalogTotals,
  companyDirectory,
  connectorManifestUrl,
  connectorMatchesQuery,
  readCatalog,
  readOperations,
} from "./catalogModel.mjs";

test("manifest URL uses the release tag and repository path", () => {
  const url = connectorManifestUrl({
    company: "GitHub",
    id: "github",
    name: "GitHub",
    description: "",
    version: "v0.5.0",
    directory: "connectors/github",
    triggers: [],
    operations: [],
  });
  assert.equal(
    url,
    "https://raw.githubusercontent.com/superdurable/dex-connectors-library/connectors/github/v0.5.0/connectors/github/connector.yaml",
  );
});

test("company directory is the first folder under connectors", () => {
  assert.equal(companyDirectory("connectors/google/gmail"), "google");
});

test("catalog search matches trigger and operation names", () => {
  const [connector] = readCatalog(`
apiVersion: connectors.dex.dev/catalog/v1alpha1
kind: ConnectorCatalog
connectors:
  - company: Google
    id: gmail
    name: Gmail
    description: Mail
    version: v0.8.0
    directory: connectors/google/gmail
    triggers:
      - name: messageReceived
        description: A message arrived
    operations:
      - name: getMessage
        kind: query
        description: Read one message
`);
  assert.equal(connectorMatchesQuery(connector, "messageReceived"), true);
  assert.equal(connectorMatchesQuery(connector, "missing"), false);
  assert.deepEqual(catalogTotals([connector]), { companies: 1, connectors: 1, operations: 1, triggers: 1 });
});

test("manifest reader keeps trigger and operation names", () => {
  const capabilities = readOperations(`
spec:
  triggers:
    - name: replyReceived
      description: A reply arrived
  operations:
    - name: createResponse
      kind: mutation
      description: Create a response
`);
  assert.deepEqual(capabilities.map((capability) => capability.name), ["replyReceived", "createResponse"]);
});
