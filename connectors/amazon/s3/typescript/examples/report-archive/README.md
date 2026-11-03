# Amazon S3 TypeScript report archive

This Flow writes one text report with create-only `putObject`, reads the stored
object's metadata with `headObject`, reads its text with `getObjectText`, and
confirms the key with `listObjects`. Only `stored`, `found`, `read`, and `found`
are wired. A provider rejection or any other optional branch fails the Flow.
The static connection name is `amazon-s3-report-archive`.

Configure that connection in Dex Web **Connections** using the
[Amazon S3 authorization guide](../../../README.md#authentication). Give it
`s3:ListBucket`, `s3:GetObject`, and `s3:PutObject` for a dedicated bucket.
Use the actual bucket name in the Flow input. The example stores no
credentials in source or Flow state.

Generate strict FDG 2.0 with a Dex CLI release that supports TypeScript source
analysis, from this package directory:

```sh
mkdir -p build
dexcli visualize ./examples/report-archive/flow/workflow.ts \
  --schema-version 2.0 --json --out ./build/amazon-s3-report-archive
```

Start `dexcli dev` with that rendering directory, set
`DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and load the private Dex project
connection through `DEX_PROJECT_*` or the local connection configuration. Then
run the Worker:

```sh
npm run example:report-archive
```

In another terminal start a Flow with the checked-in nonsecret input:

```sh
npm run example:report-archive:start -- examples/report-archive/input.example.json
```

The Client uses a unique Flow ID and prints the stored object, metadata, text,
and listing. Running the same key in a new Flow selects `alreadyExists`, which
is intentionally unwired. The integration test runs this exact Flow against a
real Dex stack and a local S3 fixture with a lost first PUT response:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21240 npm run test:integration
```

The fixture proves retry and create-only read-back. Real provider behavior is
still unverified for TypeScript.
