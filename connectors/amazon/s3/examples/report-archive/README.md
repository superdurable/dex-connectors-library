# Amazon S3 report-archive example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Amazon S3 operation:

1. `RecordReportArchiveRequest` validates the request, generates a Markdown
   report from the title and highlights, and derives the deterministic key
   `reports/<reportId>.md`;
2. `ArchiveReport` calls `s3.NewPutObjectStep` with `report-id` and
   `generator` metadata, create-only unless `shouldReplaceExisting` is true;
3. `RecordArchiveOutcome` records `archived`, or `alreadyArchived` when another
   Flow already archived a report under the key;
4. `ReadBackReportMetadata` calls `s3.NewHeadObjectStep` on the key;
5. `RecordReportMetadata` records the size, ETag, content type, and metadata;
6. `ReadBackReportText` calls `s3.NewGetObjectTextStep`;
7. `VerifyReportText` records whether the archived text equals the report this
   Flow generated;
8. `ListArchivedReports` calls `s3.NewListObjectsStep` on `reports/` with a
   `/` delimiter;
9. `CompleteReportArchive` completes with the first page of archived keys.

Only the happy-path branches and `putObject`'s `alreadyExists` are wired.
Rejected, invalid, unsupported, oversized, not-found, and defect outcomes fail
the Flow. The report text contains no timestamp, so a retried start Step
generates identical bytes and a repeated `putObject` attempt converges on the
same object.

## Configure

Follow the [Amazon S3 Connector setup](../../README.md), then configure the
`amazon-s3 / amazon-s3-reports` connection in Dex Web **Connections**. Set
`defaultBucket` to the archive bucket, or name `bucket` in the Start Flow
input. The example's Steps have no configuration units.

## Run

Generate strict FDG 2.0 from `connectors/amazon/s3` with the dexcli release
pinned in the repository's `.dex-compat-version` file:

```bash
mkdir -p build
dexcli visualize ./examples/report-archive/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/amazon-s3-report-archive
```

Run `dexcli dev` with that build directory, then run the Worker with the
connection path shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/report-archive
```

The default Worker address is `127.0.0.1:8847`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `AmazonS3ReportArchive` with a unique Flow ID:

```json
{
  "reportId": "2026-09-ops-weekly",
  "title": "Ops weekly",
  "highlights": ["Refunds above 500 need approval.", "Two incidents closed."]
}
```

`reportId` is 1 to 63 lowercase letters, digits, and hyphens. The output
reports `status`, the stored object, the read-back metadata, `isTextVerified`,
and `archivedReportKeys`. Starting a second Flow with the same `reportId`
completes as `alreadyArchived` and leaves the first report in place; add
`"shouldReplaceExisting": true` to replace it. `isFromEarlierAttempt` is true
when Dex dispatched the archive Step again and the repeated attempt found the
report its first attempt stored.

```bash
GOWORK=off go test -race ./examples/report-archive/...
GOWORK=off go test -tags=integration ./examples/report-archive/... -count=1 -v
```

The integration tests run the Flow on a real Dex Server against the
SigV4-verifying fake in `internal/s3fake`: an archived report, a second Flow
that finds it `alreadyArchived`, a replacement, an invalid report ID that sends
nothing, and an unwired rejection. Two tests delay the first PUT's response by
nine seconds, past the async local phase, so Dex dispatches the archive Step
again; in create-only mode the second PUT gets 412 and finds its own marker,
and in replace mode both PUTs carry identical bytes and headers. Either way the
Flow completes `archived` with one object. The live test in
`flow/workflow_live_test.go` runs the same Flow against a real S3 API; see the
connector README for its variables.
