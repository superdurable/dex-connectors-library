# Notion submission intake example

This operation-only example records one form submission as a row in a Notion
database and reads it back. It uses every Notion operation:

1. `search` finds the data source whose title equals `databaseTitle`. It
   ignores trashed and partial matches, such as an archived twin or a
   "Form submissions (2025)" database. With no match or several, the Flow
   completes as `dataSourceNotFound` or `dataSourceAmbiguous` and writes
   nothing.
2. `queryDatabase` reads the rows whose **Submission ID** equals the
   submission's ID.
3. With no row, `createPage` creates one. It sets the title, Submission ID,
   Email, and Submitted at, and writes the message as the page body. With one
   row, `updatePageProperties` sets that row's title, Email, and Submitted at
   to the resubmitted values. With two or more, the Flow completes as
   `duplicateRows` and changes nothing.
4. `getPage` reads the row back. The Flow checks the Submission ID and
   completes with the stored properties and body text.

The Submission ID is the business key. Notion has no idempotency key, so
`createPage` selects `uncertain` instead of resending a request Notion may
have received. The Flow then queries for the Submission ID again: it adopts a
row the uncertain create saved, or completes as `createUncertain`. Re-run the
Flow with the same Submission ID after checking Notion. The re-run updates a
row that appeared in the meantime and creates one otherwise.

A rejected create or update completes as `rejected` with Notion's
secret-safe failure. Every other optional branch is unwired and fails the
Flow, such as a `queryDatabase` rejection for a data source without the
Submission ID property.

The Flow sets its type to `NotionSubmissionIntake` and names each Step
explicitly, because Dex Web **Start Flow** uses the Flow and Step types in the
Flow Definition Graph. The start Step keeps a `WaitFor` that returns at once,
because Dex Web Start Flow invokes `WaitFor` on the start Step.

## Release baseline

- Notion Connector `v0.1.0`
- dexcli `v1.1.0` or a later stable release

## 1. Prepare the Notion database

Create a database, for example named **Form submissions**, with these
properties. The names are case-sensitive:

| Property | Type |
| --- | --- |
| the title column, under any name | Title |
| `Submission ID` | Text |
| `Email` | Email |
| `Submitted at` | Date |

Create an internal connection as the [connector README](../../README.md#notion-setup)
describes, enable **Read content**, **Update content**, and **Insert
content**, and share the database with it. The Flow writes the title through
the property ID `title`, so the title column can have any name.

## 2. Prepare a clean local test project

Dex Web configures a connection only for an exact released Connector module.
Create a separate project that consumes the release, then copy the Flow source
so dexcli analyzes it as an application dependency:

```bash
mkdir notion-submission-intake-e2e
cd notion-submission-intake-e2e
mkdir -p flow build

curl -fsSL \
  https://raw.githubusercontent.com/superdurable/dex-connectors-library/refs/tags/connectors/notion/v0.1.0/connectors/notion/examples/submission-intake/flow/workflow.go \
  -o flow/workflow.go

go mod init example.com/notion-submission-intake-e2e
go mod edit -go=1.24.0
go get github.com/superdurable/dex-connectors-library/connectors/notion@v0.1.0
go mod tidy
```

`go mod edit -go=1.24.0` keeps the project's Go version at or below the Go
release that built dexcli. Otherwise dexcli reports `go_type_check_failed`.

Generate the Flow Definition Graph used by Dex Web:

```bash
dexcli visualize ./flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/notion-submission-intake
```

The command must finish without blocking diagnostics and create
`build/notion-submission-intake.json` with `"valid": true`. The repository's
compatibility gate runs the same analysis twice from a clean consumer module
and requires identical output.

## 3. Start Dex

```bash
dexcli dev \
  --flow-rendering-dir "$PWD/build" \
  --connector-config-dir "$HOME/.dex/connectors"
```

Record the Dex Web URL and Dex Server address that dexcli prints. With the
default ports they are `http://127.0.0.1:8802` and `127.0.0.1:8801`.

## 4. Configure the connection in Dex Web

Open **Connections** and select `notion / notion-workspace`. It lists the
`search`, `queryDatabase`, `createPage`, `updatePageProperties`, and `getPage`
uses. Follow the authorization guide, paste the internal connection's API
token into **api_token**, leave the configuration defaults, and save. The
connection status should be **Ready**. No operation has Step configuration;
the database is chosen by title in the Start Flow input.

The connection file is a plaintext local-development secret store. Never commit
or share it.

## 5. Run the Worker

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
export DEX_FLOW_SERVICE_ADDRESS="127.0.0.1:8801"

GOWORK=off go run \
  github.com/superdurable/dex-connectors-library/connectors/notion/examples/submission-intake@v0.1.0
```

The Worker listens on `127.0.0.1:8851` by default. Set
`DEX_WORKER_BIND_ADDRESS` to use another address, and `DEX_BLOB_CACHE_DIR` to
move its blob cache. If Dex is unreachable, the Worker logs
`dex server unavailable; retrying` with a delay that grows to 30 seconds and
starts once Dex answers.

## 6. Start the Flow

In Dex Web, open the `NotionSubmissionIntake` Flow, choose **Start Flow**,
select the Worker address, and enter:

```json
{
  "databaseTitle": "Form submissions",
  "submissionId": "sub-2026-09-30-001",
  "name": "Ada Lovelace",
  "email": "ada@example.com",
  "message": "Hello from the form.\n\nPlease call me back."
}
```

Open the run and follow it to **Completed** with phase `created`. The display
shows the row's ID and URL, its properties as plain text, and the body read
back. Start the Flow again with the same `submissionId` and a new `email`.
That run completes with phase `updated` on the same row, and the database
still holds one row with that ID.

## Verify from the repository

The example tests run from the Connector directory:

```bash
GOWORK=off go test -race ./examples/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/submission-intake/flow/... -count=1 -v
```

The integration tests use a real Dex Server and a stateful fake Notion API.
They verify:

- a created row;
- an updated resubmission;
- duplicate rows;
- a missing or ambiguous title;
- a rejected create;
- a rate-limited create retried after `Retry-After`;
- an uncertain create reconciled by query, and one that stays uncertain;
- a create Notion saved before a 503;
- a create held for nine seconds that Notion receives once under sync
  durability;
- an update held for nine seconds that Dex dispatches twice and that
  converges;
- an unwired optional branch;
- invalid Start Flow input.

Before `v0.1.0` is published, build a local release artifact with
`go run ./cmd/connectorctl release-artifact` from the repository root and pass
its directory to `dexcli dev --connector-release-override notion=DIRECTORY`.
The test project must still resolve `connectors/notion@v0.1.0` without a
`replace`, as the compatibility gate does with a local module proxy.
