# Amazon S3 TypeScript connector

`@superdurable/connector-amazon-s3` implements the Go connector's four S3 REST
operations: `listObjects`, `headObject`, `getObjectText`, and `putObject`. The
manifest generates the operation-specific Step factories, typed models,
connections, and mock cases. The provider client signs each request with AWS
Signature Version 4 and limits response bodies. Dex owns retries; a create-only
write that encounters an existing object reads its metadata and recognizes the
same Step's `dex-idempotency-key` marker.

## Connection

Configure the static `amazon-s3-report-archive` connection in Dex Web
**Connections**. Follow the [connector authorization guide](../README.md#authentication)
for the provider-specific access key, secret, Region, endpoint, and bucket.
The access key ID, secret access key, and optional session token remain in the
private connection store and are resolved at each Invoke. Never put them in
Flow input, Attributes, Results, receipts, logs, or fixture output.

The [checked-in Worker](examples/report-archive/main.ts) loads its connection
with these lines:

```ts
const project = await loadFromEnvironment();
const connection = newProjectConnection(project, ConnectionName);
await runReportArchiveWorker(new ReportArchiveFlow(connection), stop.signal);
```

`newConnection` also binds a directly constructed `S3Client` for tests or
applications with their own secret provider. An omitted endpoint selects the
Amazon S3 Regional API; a custom loopback HTTP endpoint is intended only for
local tests. The default bucket is optional. Every operation except `putObject`
is a Query. `putObject` is a repeat-safe Mutation and returns `stored` or the
optional `alreadyExists` branch after create-only read-back. All non-happy
branches are optional and fail an example Flow when unwired.

## Example and verification

The [report archive example](examples/report-archive/) uses all four Step
factories and its static connection name. From this package directory:

```sh
npm ci
npm run check
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21240 npm run test:integration
```

Start `dexcli dev` at the address before the integration test. The test runs a
real Dex Worker and Client against a local S3 fixture, including a lost PUT
response, create-only read-back, and an unwired provider rejection. The
[Go-recorded conformance fixture](../../../../conformance/connectors/amazon/s3/scenarios.json)
checks the same provider request and output contracts in TypeScript. A unit
test compares a fixed SigV4 Authorization header to the official Go signer.
Live Amazon S3, Cloudflare R2, and MinIO authorization and object behavior
have not been verified for this TypeScript package.
