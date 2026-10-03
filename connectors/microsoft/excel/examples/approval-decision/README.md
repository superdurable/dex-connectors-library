# Microsoft Excel approval-decision example

This example runs one operation-only Flow from Dex Web **Start Flow**. It
decides a spending request against an approval policy kept in Excel and uses
every Microsoft Excel operation:

1. `RecordExcelApprovalRequest` validates the request and fails when a pick is
   missing;
2. `ReadExcelApprovalPolicy` calls `excel.NewGetTableRowsStep` on the policy
   table;
3. `DecideExcelApproval` finds the single row whose `Category` matches,
   ignoring case, and approves an amount up to `MaxAutoApproveUsd`, escalates a
   larger one to the row's `Approver`, or marks the request `needsReview`;
4. `AppendExcelDecisionRow` calls `excel.NewAppendTableRowsStep` with
   `RequestId` as the key column, so a request is logged at most once;
5. `RecordExcelDecisionLogged` records whether this Flow appended the row or
   found it already logged;
6. `UpdateExcelDecisionSummary` calls `excel.NewUpdateValuesStep` to write the
   request ID, category, amount, decision, and decision time to `A2:E2`;
7. `ReadBackExcelDecisionSummary` calls `excel.NewGetValuesStep` on the same
   range, and `CompleteExcelApprovalDecision` completes as `completed` with
   `summaryConfirmed`.

`appendTableRows`'s `uncertain` branch completes as `logUncertain` through
`ReportExcelDecisionUncertain`, so a person checks the decision log instead of
the Flow appending again. Every other non-happy branch fails the Flow.

## Prepare the workbook

In a OneDrive or SharePoint `.xlsx` workbook, create two Excel tables with
**Insert > Table**, named in **Table Design > Table Name**:

- `ApprovalPolicy` with columns `Category` (text), `MaxAutoApproveUsd`
  (number), and `Approver` (text), one row per category;
- `Decisions` with columns `RequestId`, `Requester`, `Category`, `AmountUsd`,
  `Decision`, `Approver`, and `DecidedAt`.

Add a worksheet, such as `Summary`, whose `A2:E2` the Flow overwrites with the
latest decision. The three can live in one workbook or several.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/microsoft/excel` with the latest stable
dexcli release:

```bash
dexcli visualize ./examples/approval-decision/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/microsoft-excel-approval-decision
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` for each connector Step, because a local module
is not a published release.

## Configure and run

Dex needs release metadata built from this source, passed as an override.
From the repository root, build the Studio bundle and the release:

```bash
cd "$(git rev-parse --show-toplevel)"
(cd sdk/react && npm ci && npm run build)
(cd connectors/microsoft/excel/ui && npm ci && npm run build)
mkdir -p /tmp/microsoft-excel-release
go run ./cmd/connectorctl ui-artifact --manifest connectors/microsoft/excel/connector.yaml \
  --ui-root connectors/microsoft/excel/ui/dist \
  --output /tmp/microsoft-excel-release/connector-ui.tgz \
  --digest-output /tmp/microsoft-excel-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/microsoft/excel/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/microsoft/excel \
  --version v0.1.0 --tag connectors/microsoft/excel/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/microsoft-excel-release/connector-ui.tgz \
  --ui-digest /tmp/microsoft-excel-release/connector-ui.tgz.sha256 \
  --output /tmp/microsoft-excel-release/connector-release.json \
  --digest-output /tmp/microsoft-excel-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override microsoft-excel=/tmp/microsoft-excel-release
```

Microsoft OAuth needs Dex CLI 1.4.1 or later. Open Dex Web at
`http://localhost:<port>`, follow the [Microsoft setup](../../README.md#microsoft-setup),
then open **Connections**, select `microsoft-excel / microsoft-excel-approvals`,
and authorize. The example exposes three Step configurations:

- **Approval policy workbook** and **Approval policy table** on
  `ReadExcelApprovalPolicy`;
- **Decision log workbook** and **Decision log table** on
  `AppendExcelDecisionRow`;
- **Summary workbook** and **Summary worksheet** on
  `UpdateExcelDecisionSummary`; the read-back reuses them.

Every pick is required. In a second terminal, start the Worker from
`connectors/microsoft/excel` with the connection file Dex Web shows:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/approval-decision
```

The Worker reads the picks at startup, so restart it after saving them. The
default Worker address is `127.0.0.1:8863`; override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when needed. For local
verification against a Graph-compatible fake only,
`MICROSOFT_EXCEL_LOCAL_PROVIDER_URL` sends every request for
`graph.microsoft.com` and `login.microsoftonline.com` to one loopback URL;
production Workers leave it unset.

Start `MicrosoftExcelApprovalDecision` with a unique Flow ID:

```json
{"requestId": "REQ-2026-0042", "requester": "dana@contoso.com", "category": "Travel", "amountUsd": 420}
```

The run appends a row to the real decision log and overwrites `A2:E2`; delete
the test row afterwards.

## Test

```bash
GOWORK=off go test -race ./examples/approval-decision/...
GOWORK=off go test -tags=integration ./examples/approval-decision/... -count=1 -v
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against `internal/fakeexcel`, which checks the
bearer token and stores typed input as Excel does. They prove that:

- a request is decided, logged once with literal text, summarized, and read
  back; a second Flow for the same request ID logs nothing;
- formula-like and number-like text, such as `=HYPERLINK(...)` and `00123`,
  stays text;
- an append queued for nine seconds, or answered nine seconds after applying,
  is sent once, because the Step is sync;
- an append applied before a 504 is found in the key column instead of being
  resent;
- an unconfirmed append, and a Worker lost while the append is in flight,
  complete as `logUncertain` without a second append;
- a throttled append is retried after `Retry-After` and applied once;
- a summary write that answers after nine seconds is dispatched again by async
  Dex and leaves the same values;
- a throttled read waits for `Retry-After`, a missing table fails the Flow
  through an unwired branch after one request, and an invalid request calls
  nothing.
