# Snowflake account usage summary example

This example summarizes one customer account's usage with a long Snowflake aggregate, in a Flow
started from Dex Web **Start Flow**. It never holds a Worker while the warehouse runs:

1. `RecordUsageRequest` validates the account ID and the `YYYY-MM-DD` start date.
2. `SubmitUsageQuery` uses `submitStatement` to send `UsageSummaryStatement` with both values bound
   to `?` placeholders. Snowflake returns a statement handle right away.
3. `RecordSubmittedQuery` stores the handle, and `WaitForUsageQuery` waits on a durable Timer.
4. `ReadUsageResult` uses `getStatementResult`. While Snowflake reports `running`,
   `RecordRunningQuery` counts the read and returns to the Timer.
5. On `completed`, `CompleteUsageSummary` decodes the one row and completes with `completed`.
6. On `providerRejected`, `RecordFailedQuery` completes with `failed` and Snowflake's code and
   SQLSTATE; a refused submission completes with `rejected` through `RecordRejectedQuery`.
7. After the last allowed running read, `CancelUsageQuery` uses `cancelStatement`, and
   `RecordWaitBudgetExhausted` completes with `waitBudgetExhausted` and whether Snowflake accepted
   the cancel. A rejected cancel usually means the statement finished meanwhile.

`DefaultStatusPolicy` reads every 15 seconds and cancels after 40 running reads, about ten minutes.
The `truncated`, `invalidResponse`, and `defect` branches are not wired and fail the Flow.

## Prepare Snowflake

Create the table, or point the statement at your own usage table, and grant the connection's role
only what the example needs:

```sql
CREATE TABLE ANALYTICS.BILLING.USAGE_EVENTS (
  ACCOUNT_ID VARCHAR NOT NULL, EVENT_AT TIMESTAMP_NTZ NOT NULL, CREDITS_USED NUMBER(12,4) NOT NULL);
GRANT USAGE ON WAREHOUSE REPORTING_WH TO ROLE DEX_REPORTING;
GRANT USAGE ON DATABASE ANALYTICS TO ROLE DEX_REPORTING;
GRANT USAGE ON SCHEMA ANALYTICS.BILLING TO ROLE DEX_REPORTING;
GRANT SELECT ON TABLE ANALYTICS.BILLING.USAGE_EVENTS TO ROLE DEX_REPORTING;
```

## Run locally

Run these commands from `connectors/snowflake`. Generate strict FDG 2.0:

```bash
mkdir -p /tmp/snowflake-usage/graphs /tmp/snowflake-usage/release
dexcli visualize ./examples/account-usage-summary/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/snowflake-usage/graphs/account-usage-summary
```

Until the connector is released, build a local release override from the repository root:

```bash
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/snowflake/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/snowflake \
  --version v0.21.0 --tag connectors/snowflake/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/snowflake-usage/release/connector-release.json \
  --digest-output /tmp/snowflake-usage/release/connector-release.json.sha256
dexcli dev --open=false --flow-rendering-dir /tmp/snowflake-usage/graphs \
  --connector-release-override snowflake=/tmp/snowflake-usage/release
```

Open Dex Web at `http://127.0.0.1:8802`. In **Connections**, open `snowflake / snowflake-warehouse`,
choose **Key-pair authentication** or **Programmatic access token**, follow the authorization
guide, and enter `accountIdentifier`, `warehouse` (`REPORTING_WH`), `role` (`DEX_REPORTING`),
`database` (`ANALYTICS`), and `schema` (`BILLING`). Keep the other defaults. The connection must
show **Ready** and **Local override**. Dex Web or Superverse Studio writes the connection to the
project configuration. In another terminal, start the Worker with the `DEX_PROJECT_*` environment
that names that configuration, as
[project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
go run ./examples/account-usage-summary
```

In **Start Flow**, choose `SnowflakeAccountUsageSummary` and the Worker at `127.0.0.1:8857`, and
start it with a unique Flow ID:

```json
{"accountId": "acct_003", "since": "2026-01-01"}
```

**Display** shows `running` and the statement handle while the warehouse works, then `completed`
with `eventCount` and `creditsUsed` as exact decimal strings and `lastEventAt` in ISO 8601. Look up
the handle in Snowsight under **Monitoring > Query History** to confirm one execution.

## Test

```bash
GOWORK=off go test -race ./examples/account-usage-summary/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/account-usage-summary/... -count=1
```

The integration suite runs the Flow on a real Dex Worker against the scripted SQL API in
`internal/fakesnowflake`. It covers:

- two `running` reads (202) and then the rows (200), with exactly one execution;
- a failed statement (422) recorded with its code and SQLSTATE and no message text;
- a refused submission (422 at submit) that runs nothing;
- a statement that outlasts the wait budget and is canceled;
- a submission that answers after nine seconds, which Dex dispatches twice with the same
  `requestId` while the scripted API runs the statement once;
- a Worker force-stopped during the durable wait and replaced, which resumes without resubmitting;
- invalid input rejected before Snowflake.
