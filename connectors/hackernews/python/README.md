# Hacker News connector for Python

Install with `pip install dex-connector-hackernews==0.2.0`. The package
requires `dex-connectors-sdk>=0.8.0,<1` and `dex-python-sdk>=1.28.0`, and
implements the manifest Queries `listStoryIDs` and `getItem` with the same
branches, defaults, and failure classification as the Go module. The public
API needs no credentials.

Operations use the manifest identity: connector ID `hacker-news-daily` and
provider `hacker-news`, as `CONNECTOR_ID` and `PROVIDER`. `new_project_connection`
selects the Dex Web connection with that connector ID and provider, and Call IDs
match the Go module. Version 0.1.0 used `hackernews` for both, so it could not
open a Dex Web connection; 0.2.0 changes that identity (breaking).

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listStoryIDs` | `new_list_story_ids_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `getItem` | `new_get_item_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`listStoryIDs` reads the `top` (default), `best`, `new`, `ask`, or `show` feed
and keeps the first `limit` IDs (default 50, at most 500), reporting whether
more remained. `getItem` returns a public story, comment, job, poll, or poll
option with its discussion URL; a missing, deleted, or dead item selects
`notFound`. 408, 429, 5xx, and transport failures retry; other non-200
responses select `providerRejected`. Item `text_html` is untrusted provider
HTML.

## Agent tools

Both Queries set `agent.exposed` in the package's `dex-connector.json`, so an
agent host can offer `client.list_story_ids()` and `client.get_item()` as read
tools. Every optional branch except `invalidResponse` is classified `rejected`:
a read changes nothing. `invalidResponse` is a provider malfunction the model
cannot fix, so it stays unlisted and agent hosts record it as a failure.

## Example

[`examples/top_story`](examples/top_story/flow.py) reads a feed's first story ID
and then that story. Run the Worker with
`uv run python -m examples.top_story.worker` after `dexcli dev` has loaded a
project containing the `hackernews` connection. It is smaller than the Go
daily-digest example, which also summarizes stories with an LLM.

## Tests

`hackernewsmock.Mock` scripts both Queries and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
