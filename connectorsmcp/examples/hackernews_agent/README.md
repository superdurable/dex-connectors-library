# Hacker News agent over dex-connectors-mcp

A Dex agent session that reads Hacker News through the Dex Connectors MCP
server. The agent is a dex-agents built-in loop session (`AgentSessionFlow`,
Flow type `HackerNewsMcpAgent`). Its tools come from `McpToolSource`, which
lists a locally started, released `dex-connectors-mcp serve` and runs each MCP
tool call in its own Dex tool Step:

1. `hackernews__hacker-news-daily_list_story_ids` with `{"feed": "top", "limit": 3}`.
2. `hackernews__hacker-news-daily_get_item` with the first ID.
3. The answer: `Top Hacker News story <id>: <title>`.

`McpToolSource` prefixes each MCP tool name with its server name
(`hackernews__`). Both tools declare `io.superdurable.dex/effect: read`, so
each runs in a `CallTool` Step with up to three attempts, and every attempt
sends the same `io.superdurable.dex/idempotencyKey`,
`<flowId>/<turnId>/<toolCallId>`.

The model is a deterministic `ScriptedModelClient`
([`hackernews_agent/agent.py`](hackernews_agent/agent.py)): it needs no model
key or account, and a retried `RunModel` sends the same calls. Hacker News
needs no credential either. The connection is a `connections.json` record
named `hacker-news-public` with empty settings, written with mode `0600`.

## Requirements

- Python 3.11 or later and `uv`.
- `dexcli` from a stable release with `dex-server-zd` beside it. From the
  repository root: `python3 script/dex_compatibility.py install-dexcli --output .bin/dexcli`.
- The released `dex-connectors-mcp`, verified against the release index:
  `python3 tools/install_connectors_mcp.py --output .bin/dex-connectors-mcp`.
- For the tests only, Go 1.25 or later to build the notes fixture server.

## Run it against `dexcli dev`

```bash
# Terminal 1, repository root:
.bin/dexcli dev -open=false

# Terminal 2, this directory:
uv sync --locked
python3 tools/install_connectors_mcp.py --output .bin/dex-connectors-mcp
DEX_AGENTS_OPEN_BROWSER=0 uv run python -m hackernews_agent.main --connectors-mcp .bin/dex-connectors-mcp
```

[`hackernews_agent/main.py`](hackernews_agent/main.py) starts
`dex-connectors-mcp serve --listen 127.0.0.1:0 --output json
--exit-on-stdin-close` with a private state directory and connections file,
reads the `url` of its ready line, starts a Worker and a Client in the same
process, starts a session with a unique ID, polls the session snapshot until
the first turn finished, prints the conversation, and stops the session.
`--dex-address` selects a `dexcli dev` on another port (default
`127.0.0.1:8801`). The session stays visible in Dex Web as
`hackernews-agent-<uuid>`.

## Tests

```bash
make test-connectors-mcp-example   # from the repository root
```

The target builds the notes fixture server, installs the newest released
`dex-connectors-mcp`, checks formatting, and runs
[`tests/test_hackernews_agent.py`](tests/test_hackernews_agent.py). Each test
starts its own `dexcli dev --engine zd` on free ports through
`dex_agents.testing`, and the MCP servers. Every session ID is unique, and
every wait polls durable state with a deadline.
`DEXCLI`, `DEX_CONNECTORS_MCP`, and `NOTES_FIXTURE_SERVER` override the
executables; a missing one fails the suite with how to install it.

- **The entrypoint, through the released server.** The test calls
  `run_hacker_news_agent` from `main.py`, with its own Worker and Client. The
  agent lists and reads the top story; each `CallTool` Step completes on its
  first attempt.
- **A retried Step resends its key; a write is replayed.** The session runs
  on a Worker child ([`tests/agent_worker.py`](tests/agent_worker.py)), and
  [`tests/fault_proxy.py`](tests/fault_proxy.py) sits between it and each
  server. It forwards the first `hacker-news-daily_get_item` call and the
  first `example-notes_save_record` call, lets the server finish them, and
  answers the Worker `502`. Dex retries each `CallTool` Step, and the test
  asserts:
  - both attempts of each call carried the same key, `<flowId>/turn-1/<toolCallId>`;
  - the read ran again: dex-connectors-mcp keeps no call state for reads, so a
    read is never answered as a replay;
  - the write's retry was answered from the stored result, with
    `io.superdurable.connectors/replayed: true` and the first attempt's
    `structuredContent`;
  - the fake provider received the write once;
  - Dex history shows `CallTool` final attempts `[1, 2, 2]`.

The released server links no credential-free write: Hacker News offers only
reads, and Slack's writes need a Slack account. The replay therefore uses
[`notes_fixture_server`](notes_fixture_server/main.go), a test fixture in this
module that runs the same `serve` code with the fake Example Notes connector
and fake provider of `internal/testconnectors`. It offers only
`example-notes_save_record`, a declared `idempotent_write`. It is never
released.
