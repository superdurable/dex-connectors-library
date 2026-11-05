# Google Docs TypeScript connector

`@superdurable/connector-google-docs` provides operation-specific Dex Steps
for `getDocumentText`, `createDocument`, `replaceDocumentText`, and `appendText`.
The manifest supplies generated models, factories, mocks, and refresh wiring
for Google OAuth and Workspace domain delegation. The provider client bounds
responses and text, reads only the first tab, and uses revision guards for
writes. `createDocument` uses a single-dispatch fence and Drive app-property
lookup to recover an earlier create without repeating an uncertain write.

The runnable [policy documents example](examples/policy-documents/README.md)
uses the static `policy-docs` connection and exercises every operation through
a Dex Worker. Its input file contains no credentials.

Run `npm ci && npm run check`, then run `npm run test:integration` with a local
`dexcli dev` and `DEX_FLOW_SERVICE_ADDRESS` set to its gRPC address. The
conformance test replays Go-recorded provider exchanges. Live Google provider
authorization and calls have not been verified.
