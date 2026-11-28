# Hacker News connector for Python

Install with `pip install dex-connector-hackernews==0.4.0`. The package
requires `dex-connectors-sdk>=0.8.0,<1` and `dex-python-sdk>=1.28.0`, and
implements all six manifest Queries with the same
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
| `searchItems` | `new_search_items_step` | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `searchStories` | `new_search_stories_step` | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `getArticleThread` | `new_get_article_thread_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getUser` | `new_get_user_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`listStoryIDs` reads the `top` (default), `best`, `new`, `ask`, or `show` feed
and keeps the first `limit` IDs (default 50, at most 500), reporting whether
more remained. Both fields are optional in the published input schema, so an
agent call may omit either or both. `getItem` returns a public story, comment, job, poll, or poll
option with its discussion URL; a missing, deleted, or dead item selects
`notFound`. 408, 429, 5xx, and transport failures retry; other non-200
responses select `providerRejected`. Item `text_html` is untrusted provider
HTML.

## Agent tools

All six Queries set `agent.exposed` in the package's `dex-connector.json`.
Agent hosts can expose them as read tools. The manifest classifies optional
business branches as `rejected` because a read changes nothing. `invalidResponse` is a provider malfunction the model
cannot fix, so it stays unlisted and agent hosts record it as a failure.

## Example

[`examples/top_story`](examples/top_story/flow.py) reads a feed's first story ID
and then that story. Run the Worker with
`uv run python -m examples.top_story.worker` after `dexcli dev` has loaded a
project containing the `hackernews` connection. It is smaller than the Go
daily-digest example, which also summarizes stories with an LLM.

The same Worker registers `HackerNewsProfilePython`. Start it with
`{"username":"dhouston"}` to read the public profile, karma, and submitted item
IDs. The connector builds the profile permalink from the requested username.
Sanitize the untrusted `User.about` HTML before displaying it.
[Runnable profile Flow](examples/top_story/profile_flow.py)

With Dex running, `uv run python -m examples.top_story.profile_integration`
checks the retry path. Append a public username to run against Firebase.
Generate the graph from this package with
`dexcli visualize examples/top_story/profile_flow.py --schema-version 2.0 --lint app --json --open=false --python .venv/bin/python --out /tmp/hackernews-python-profile`.

For a custom test connection that exercises profiles, pass the `get_user`
Query by keyword to `new_connection_with_operations`.

The Worker also registers `HackerNewsArticleThreadPython`. Start it with
`{"article_id":8863}`. The Query reads the article and nested comments
from the configured Search API endpoint in one request. The result preserves
the indexed comment order and untrusted HTML. Bind `get_article_thread` by
keyword when constructing an operation-based test connection for this Flow.
[Runnable article thread Flow](examples/top_story/article_flow.py)

With Dex running, verify a retry using `uv run python -m examples.top_story.article_integration`.
Append `8863` to read the public article and print its discussion URL and top-level comment count.
Generate the graph with
`dexcli visualize examples/top_story/article_flow.py --schema-version 2.0 --lint app --json --open=false --python .venv/bin/python --out /tmp/hackernews-python-thread`.

The Worker also registers `HackerNewsStorySearchPython` and `HackerNewsItemSearchPython`.
Start either with `{"query":"compiler","page":0}`. Story search filters for stories.
Item search includes indexed comments, polls and poll options. Each Flow reads one
page with two hits. The result carries the next page number. Both Queries accept
timezone-aware Python datetimes for creation bounds at microsecond precision.
[Story search Flow](examples/top_story/story_search_flow.py) · [Item search Flow](examples/top_story/item_search_flow.py)

With Dex running, `uv run python -m examples.top_story.search_integration` checks a
503 followed by a successful page for each Flow. Append `compiler` for public reads.
Generate the two graphs from this package.

```sh
for kind in story item; do
  dexcli visualize "examples/top_story/${kind}_search_flow.py" --schema-version 2.0 --lint app --json --open=false --python .venv/bin/python --out "/tmp/hackernews-${kind}-python"
done
```

## Tests

`hackernewsmock.Mock` scripts all six Queries and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
