# Hacker News Daily

This runnable Dex Flow samples Hacker News hourly and generates a daily
developer digest. It uses the local Codex CLI login for summaries. No separate
model API key is required. The Worker host must have a working `codex exec`
installation and login. The example was exercised with Codex CLI `0.157.1` and
Dex CLI `v0.13.8`.

## Run locally

Run these commands from the repository root. Install Dex CLI and its Temporal
CLI dependency first. Verify the existing Codex login:

```sh
codex login status
mkdir -p /tmp/hn-daily/graphs /tmp/hn-daily/release
dexcli visualize connectors/hackernews/examples/daily-digest/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/hn-daily/graphs/daily
```

For an unreleased checkout, build a local release override. This does not
publish a release or modify the Go module cache:

```sh
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/hackernews/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/hackernews \
  --version v0.21.0 --tag connectors/hackernews/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/hn-daily/release/connector-release.json \
  --digest-output /tmp/hn-daily/release/connector-release.json.sha256
dexcli dev --open=false --flow-rendering-dir /tmp/hn-daily/graphs \
  --connector-release-override hacker-news-daily=/tmp/hn-daily/release
```

Open Dex Web at `http://127.0.0.1:8802`. In **Connections**, save
`hacker-news-public` with the default configuration and no credentials. The
connection must show **Ready** and **Local override**. Dex Web or Superverse
Studio writes the connection to the project configuration. In another terminal,
start the Worker from the connector module with the `DEX_PROJECT_*` environment
that names that configuration, as
[project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```sh
cd connectors/hackernews
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
go run ./examples/daily-digest
```

In **Start Flow**, choose `HackerNewsDaily` and the healthy Worker at
`127.0.0.1:8818`. This input produces one current snapshot and completes:

```json
{"once": true}
```

The output language defaults to English. For a Chinese preview, use:

```json
{"once": true, "language": "Chinese"}
```

For a continuous monitor, use `{}`. The first report is due 24 hours after
start. Use `firstDigestAt` to choose its first publication time with an RFC3339
timestamp. Subsequent reports follow a fixed 24-hour cadence, not a local
wall-clock schedule that adjusts for daylight saving time.

## Optional input

| Field | Default | Meaning |
| --- | --- | --- |
| `interests` | General developer topics | Natural-language interests; at most 2000 bytes |
| `exclude` | Empty | Topics to omit; at most 2000 bytes |
| `language` | `English` | Output language; at most 100 bytes |
| `maxItems` | `8` | Maximum entries, from 1 through 10 |
| `firstDigestAt` | Start + 24 hours | First publication time; `once` changes the default to now |
| `once` | `false` | Complete after the first report |

All fields are optional. Empty `language` also selects English. Interest
matching is semantic. No model family, company, or keyword is a fixed target.
The summarizer can return fewer entries when useful evidence is sparse.

## Collection and publication

Each sample reads 30 top IDs and 10 IDs from each of best, show, ask, and new.
It deduplicates IDs before reading items, excludes jobs and removed items, and
keeps stories created in the past 48 hours. The candidate pool retains at most
40 stories: up to 30 by score, then the most recent remaining stories. This
gives new discoveries space without depending entirely on popularity.
Identical original URLs are collapsed, ignoring URL fragments and host case.

Before publication, the Flow reads at most the first three ranked top-level comments
for each candidate. It sends bounded post and comment text to Codex. This is a
sample of viewpoints, not a full thread or a consensus measurement. External
articles are not fetched. The prompt requires summaries to identify their
evidence basis and attribute comment claims.

Codex runs in a temporary directory with a read-only sandbox, structured JSON
output, and shell, apps, plugins, multi-agent, and web search disabled. It uses
the CLI's default model and existing authentication. It ignores user CLI
configuration. Credentials stay outside Flow state. See the
[official non-interactive mode documentation](https://developers.openai.com/codex/noninteractive/).

The Flow validates story and comment IDs before it renders a report. Links
come from HN records and follow each summary. Text posts can omit the original
link. Published IDs and original URLs are suppressed for seven days. The
latest report is available through `GetDexSummary`, `GetDexDisplay`, and the
completion result when `once` is true. Dex Web exposes the Markdown in the
report's `markdown` field.

Dex persists the timer, candidate pool, pending reads, publication history,
and latest report. Keep the Dex Server and Worker running for automatic
reports. Worker replacement does not reset a persisted timer. After downtime,
the Flow produces the due report and advances to the next future daily slot;
it does not invent missed snapshots or backfill historical HN rankings.
Sampling can miss stories between polls. Only the latest report is retained
as the display value; external delivery and a report archive are outside this
example.

HN queries use the connector's bounded retries. The Codex step uses synchronous
durability, a five-minute Execute timeout, a four-minute process timeout, and
at most two attempts. A retry can repeat generation, but the report and its
deduplication history are committed together. Unknown source IDs or invalid
citation relationships fail the Flow before publication.

## Test

Start a separate Dex development stack, then run the repository targets from
the repository root. These tests use public API fakes and an injected
summarizer, so they do not consume model quota:

```sh
make test-connectors
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 make test-connector-integration
```

A live acceptance run uses the same **Connections**, Worker health, and
**Start Flow** endpoints as Dex Web. Inspect the terminal result and the exact
original, discussion, and cited comment links before publishing a change.
