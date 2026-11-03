# Hacker News connector for Python

Install with `pip install dex-connector-hackernews==0.1.0`. The package
requires `dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and
implements the manifest Queries `listStoryIDs` and `getItem` with the same
branches, defaults, and failure classification as the Go module. The public
API needs no credentials.

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

`Client` accepts an optional `now` callable for deterministic feed observation
times and `Retry-After` parsing in provider tests. Normal callers omit it.

## Example

[`examples/top_story`](examples/top_story/flow.py) reads a feed's first story ID
and then that story. Run the Worker with
`uv run python -m examples.top_story.worker` after `dexcli dev` has loaded a
project containing the `hackernews` connection. It is smaller than the Go
daily-digest example, which also summarizes stories with an LLM.

## Tests

`hackernewsmock.Mock` scripts both Queries and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks, including Go-recorded conformance scenarios with a fixed clock.
