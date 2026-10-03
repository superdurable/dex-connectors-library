# Google Docs policy-publish example

This example runs one operation-only Flow from Dex Web **Start Flow**. It
publishes a policy from a Google Docs template and uses every Google Docs
operation:

1. `RecordPublishRequest` validates and persists the title and placeholder
   values, and fails when no template is configured;
2. `ReadPolicyTemplate` calls `docs.NewGetDocumentTextStep` on the saved
   template and renders it as Markdown without pending suggestions;
3. `PreparePolicyDraft` records the template revision and builds the create;
4. `CreatePolicyDocument` calls `docs.NewCreateDocumentStep` with the template
   Markdown in the saved destination folder and keeps its Result in an
   Attribute;
5. `ReadPolicyDraft` calls `docs.NewGetDocumentTextStep` on the new document
   to learn its revision;
6. `PreparePlaceholderFill` pairs that revision with the placeholder values;
7. `FillPolicyPlaceholders` calls `docs.NewReplaceDocumentTextStep`, which
   Google applies only at that revision;
8. `PreparePublicationStamp` builds the stamp at the revision the fill produced;
9. `AppendPublicationStamp` calls `docs.NewAppendTextStep`;
10. `ReadBackPolicy` calls `docs.NewGetDocumentTextStep` on the result;
11. `CompletePublication` completes as `published` with the read-back Markdown.

`createDocument`'s `uncertain` branch completes as `creationUncertain`, so a
person checks Drive instead of the Flow creating a second document.
`replaceDocumentText`'s `placeholderNotFound` branch completes as
`placeholderMissing` with the missing placeholders and nothing filled. Every
other non-happy branch, including `revisionChanged` when someone edits the new
document between Steps, fails the Flow.

## Configure

Follow the [Google Docs Connector setup](../../README.md), then configure the
`google-docs / google-docs-policies` connection in Dex Web **Connections**. The
example exposes two units:

- **Policy template** on `ReadPolicyTemplate`, a `documentPicker`: the Google
  Doc every published policy copies, with placeholders such as
  `{{effectiveDate}}`. It is required.
- **Destination folder** on `CreatePolicyDocument`, a `folderPicker`: the Drive
  folder that receives each policy. Blank creates it in the My Drive root.

The Worker loads both picks once at startup, so restart it after saving them.

## Run

Generate strict FDG 2.0 from `connectors/google/docs` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/policy-publish/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/google-docs-policy-publish
```

Run `dexcli dev` with that build directory, then run the Worker with the
connection path shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/policy-publish
```

The default Worker address is `127.0.0.1:8823`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `GoogleDocsPolicyPublish` with a unique Flow ID:

```json
{
  "title": "Acme Refund Policy 2026",
  "placeholders": [
    {"placeholder": "{{effectiveDate}}", "text": "2026-10-01"},
    {"placeholder": "{{companyName}}", "text": "Acme"}
  ]
}
```

Every placeholder must occur in the template. The Flow output reports the
template revision, the created document, whether a retried Step reused an
earlier attempt's work, and the published Markdown.

```bash
GOWORK=off go test -race ./examples/policy-publish/...
GOWORK=off go test -tags=integration ./examples/policy-publish/... -count=1 -v
```

The integration tests run the Flow on a real Dex Server against a stateful
fake Docs and Drive provider that applies a batch only at its required
revision. They prove that:

- a create that answers after nine seconds is sent once, because the Step is
  sync;
- a create applied before a lost response is found again by its app property;
- an unconfirmed create, and a Worker lost while the create is in flight,
  complete as `creationUncertain` without a second create;
- a fill or stamp that answers after nine seconds is dispatched again by Dex
  and still applied once, and a delayed fill that reaches Google after the
  backup dispatch's fill is rejected for its stale revision;
- a missing placeholder writes nothing, and an invalid request calls nothing.
