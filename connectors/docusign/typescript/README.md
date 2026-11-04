# DocuSign TypeScript connector

`@superdurable/connector-docusign` provides five eSignature operations for Dex Flows: `createEnvelopeFromTemplate`, `getEnvelope`, `listEnvelopeRecipients`, `voidEnvelope`, and `downloadCombinedDocument`. The package uses the manifest's production and developer OAuth methods, with automatic refresh through the generated credential wiring. DocuSign Connect trigger delivery remains Go-only.

The package exports generated operation-specific Step factories, typed models, mock cases, `DocuSignClient`, and `newProjectConnection`. Applications configure a named DocuSign connection in Dex Web using the [connector authorization guide](../README.md). Never put access tokens, refresh tokens, client secrets, Connect HMAC keys, or signed PDFs in Flow inputs, Attributes, Results, or logs.

The [envelope status example](examples/envelope-status/README.md) is a runnable Flow, Worker, and Client using the static `docusign-envelope-reader` connection. Its integration test runs through a real Dex Worker and Client with a local DocuSign stand-in. Provider replay scenarios under [`conformance/connectors/docusign`](../../../conformance/connectors/docusign/) check Go and TypeScript request and classification parity. Real DocuSign OAuth authorization, regional account lookup, eSignature requests, and Connect configuration require verification in Dex Web with a user-controlled provider account; they were not exercised by these local tests.

The optional `CombinedDocumentStore` supplied to `DocuSignClient` stores a downloaded PDF outside Dex and returns a safe location of at most 1024 bytes. Without a store, `downloadCombinedDocument` returns only its byte count and SHA-256 digest. A store must consume the whole content atomically and be safe to call again after a retry.

From this package directory, run `npm ci`, `npm run check`, and `npm run test:integration` with `DEX_FLOW_SERVICE_ADDRESS` pointing to a running `dexcli dev` server.
