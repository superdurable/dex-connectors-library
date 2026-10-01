# Google Forms response-recorder example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Google Forms operation to record each new response's answers:

1. `RecordRecorderRequest` checks that a form was picked and that the input
   sets a `cursor` or a `responseId`, not both, and records the request;
2. `ReadForm` calls `forms.NewGetFormStep` for the form picked in its
   `formPicker` unit;
3. `IndexFormQuestions` records every question's ID, title, and type, labeling
   a grid row "Grid prompt / Row", then continues to one of two paths;
4. `ListNewResponses` calls `forms.NewListResponsesStep` with
   `timestamp >= cursor.lastSubmittedAt`, 25 responses per page;
5. `RecordResponsePage` records each response the cursor has not seen, with its
   answers in form order, and reads the next page, up to eight pages;
6. `ReadNamedResponse` calls `forms.NewGetResponseStep` when the input names a
   `responseId`; `RecordNamedResponse` records it, and
   `ReportResponseNotFound` completes as `responseNotFound` when it does not
   exist.

Only the happy-path branches and `getResponse`'s `notFound` are wired. A form
that is gone or unreadable, a rejected or invalid read, and a defect fail the
Flow.

## The cursor

Google filters responses by submission time, and several responses can share
one instant. Each `recorded` run therefore completes with a `nextCursor`: the
latest `lastSubmittedAt` it recorded and every response ID at exactly that
time. Pass it as the next run's `cursor`. That run lists "at or after" the
time and skips those IDs, so a response that arrives at the same instant later
is still recorded, and nothing is recorded twice.

A respondent who edits a response submits it again with a later
`lastSubmittedAt`, so the next run records it again with `isEdited: true`.

Google documents no listing order. A response on an unread page can be older
than every response already read, so the cursor advances only after the last
page. When more than eight pages match, the run completes as `truncated` and
returns the input cursor unchanged; start again from a later cursor, or raise
`MaxResponsePages`.

## Configure

Follow the [Google Forms Connector setup](../../README.md), then configure the
`google-forms / google-forms-intake` connection in Dex Web **Connections**.
Use an account that can edit the form; Google Forms shows responses to a form's
editors.

The example exposes one required `formPicker` unit, **Form**, on `ReadForm`.
Choose **Choose form** to list the account's forms from Google Drive, or paste
a form ID or its `https://docs.google.com/forms/d/FORM_ID/edit` link. The
responder link `.../forms/d/e/ID/viewform` carries a different ID and is
rejected. The Worker loads the pick once at startup, so restart it after
saving. A Worker started without a pick fails each run at its first Step.

## Run

Generate strict FDG 2.0 from `connectors/google/forms` with the dexcli release
pinned in the repository's `.dex-compat-version` file
(`python3 script/dex_compatibility.py install-dexcli --output <path>`
installs it):

```bash
mkdir -p build
dexcli visualize ./examples/response-recorder/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/response-recorder
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required`, because a local module is not a published
release.

Dex needs release metadata and the Studio bundle built from this source;
without them the connection shows **Unsupported**. From the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
npm ci --prefix sdk/react && npm run build --prefix sdk/react
npm ci --prefix connectors/google/forms/ui && npm run build --prefix connectors/google/forms/ui
mkdir -p /tmp/google-forms-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/google/forms/connector.yaml \
  --ui-root connectors/google/forms/ui/dist \
  --output /tmp/google-forms-release/connector-ui.tgz \
  --digest-output /tmp/google-forms-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/google/forms/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/google/forms \
  --version v0.1.0 --tag connectors/google/forms/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/google-forms-release/connector-ui.tgz \
  --ui-digest /tmp/google-forms-release/connector-ui.tgz.sha256 \
  --output /tmp/google-forms-release/connector-release.json \
  --digest-output /tmp/google-forms-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/google/forms/build" \
  --connector-release-override google-forms=/tmp/google-forms-release
```

Then run the Worker with the connection path shown by Dex Web:

```bash
cd connectors/google/forms
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/response-recorder
```

The default Worker address is `127.0.0.1:8823`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `GoogleFormsResponseRecorder` with a unique Flow ID. `{}` records every
response. A later run passes the previous run's `nextCursor`:

```json
{
  "cursor": {
    "lastSubmittedAt": "2026-09-30T09:15:00.123456Z",
    "responseIds": ["ACYDBNj_shared_a", "ACYDBNj_shared_b"]
  }
}
```

`{"responseId": "ACYDBNj_r1"}` records one response instead. The Flow output
lists each recorded response's ID, submission times, `isEdited`, respondent
email when the form collects it, and answers labeled with their question text.

```bash
GOWORK=off go test -race ./examples/response-recorder/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/response-recorder/... -count=1 -v
```

The integration tests run the Flow on a real Dex Server against a stateful
fake Google Forms provider that pages two responses at a time and lists newest
insertion first, not by time. They cover:

- three runs that hand the cursor on, with two responses at one instant, a late
  arrival at that instant, and an edit;
- one response by ID, a missing ID, and a request that sets both inputs;
- a listing longer than the page budget, which keeps the input cursor;
- a rate-limited form read retried after `Retry-After`;
- a Worker without a form pick and a deleted form;
- a nine-second listing that Dex dispatches again, recorded once.
