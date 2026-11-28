# Hacker News TypeScript connector

The public Hacker News API connector reads feed IDs and item details with Dex Query Steps. Install `@superdurable/connector-hackernews` and declare a `hacker-news` connection named `hackernews-public` in your Dex project. The checked-in [top-story example](examples/top-story/README.md) shows the typed Flow and Worker.

`HackerNewsClient` accepts `baseUrl`, `searchBaseUrl`, `timeoutMs`, and `maxResponseBytes`; omitted values use the manifest defaults. `newProjectConnection` reads the named Dex project connection, including the manifest `timeout` in nanoseconds. The provider is public, so no credential is required. Provider calls occur only inside Step execution. `listStoryIDs` accepts top, best, new, ask, or show and a limit from 1 to 500; `feed` and `limit` are optional, and omitting them selects top and 50. Version 0.3.0 types `ListStoryIDsInput.feed` and `limit` as optional (breaking): code that implements or reads `ListStoryIDsInput` must handle `undefined`. `getItem` reads one positive item ID and supplies its Hacker News discussion URL. Missing, deleted, and dead items select `notFound`; rate limits and availability failures retry.

From 0.4.0, `newSearchStoriesStep` and `SearchStoriesOperation` search stories through the Hacker News Search API, with the same input, output, validation, and branches as the Go `searchStories` (see the [connector README](../README.md)). `SearchStoriesInput` takes an optional `query` of at most 512 bytes, `sortBy` (`relevance` by default, or `date`), `tags` (`show_hn`, `ask_hn`, `front_page`; `story` is always applied), strict `createdAfter` and `createdBefore` RFC 3339 bounds, `minPoints`, and a zero-based `page` with `hitsPerPage` from 1 to 100 (default 20); `page` times `hitsPerPage` must stay below 1000. `StorySearchPage` returns `stories`, `page`, `pages`, `totalHits`, `hitsPerPage`, and `nextPage` (zero after the last page); each `StorySearchHit` carries the story ID, title, URL, discussion URL, author, points, comment count, creation time, tags, and plain `storyText` of at most `MaxStoryTextCharacters` (2000) code points. All six operations are exposed to agent hosts through `@superdurable/connector-hackernews/dex-connector.json`. The [story-search example](examples/story-search/README.md) reads pages until the last one or a page budget. The default mock serves the requested zero-based page from the manifest. Use `searchStoriesSearched` to supply a custom page.

From 0.5.0, `newGetUserStep` reads a case-sensitive username and returns its public profile, karma, submitted IDs, and canonical permalink. Sanitize the provider's `User.about` HTML before displaying it. Custom operation bindings must implement `getUser()`, `getArticleThread()`, and `searchItems()`.

The top-story Worker also registers `HackerNewsProfileTypeScript`. Start it with `{"username":"dhouston"}`. With a local Dex server running, `npm run integration:profile` checks the retry path. Append `-- dhouston` for a live read. For FDG analysis from a source checkout, expose the local package manifest once with `ln -s ../.. node_modules/@superdurable/connector-hackernews` after `npm ci`. Generate the graph with `dexcli visualize examples/top-story/profile-flow.ts --schema-version 2.0 --lint app --json --open=false --out /tmp/hackernews-typescript-profile`. [Runnable profile Flow](examples/top-story/profile-flow.ts)

The Worker registers `HackerNewsArticleThreadTypeScript`. Start it with `{"articleId":8863}`.
The `newGetArticleThreadStep` factory reads the article and its indexed comment tree from
the configured Search API endpoint in one request. The output preserves comment order
and untrusted provider HTML. With Dex running, `npm run integration:article` verifies
a retry. Append `-- 8863` to read the public article and print its discussion URL and
top-level comment count. Generate the graph with
`dexcli visualize examples/top-story/article-flow.ts --schema-version 2.0 --lint app --json --open=false --out /tmp/hackernews-typescript-thread`.
[Runnable article thread Flow](examples/top-story/article-flow.ts)

The `newSearchItemsStep` factory searches indexed stories, comments, polls and poll options.
The `HackerNewsItemSearchTypeScript` example takes `{"query":"compiler","page":0}`
and returns a two-hit page with a continuation number. Its deterministic retry test
also checks the encoded query received by the provider. Run `npm run integration:item-search`
with Dex running, or append `-- compiler` for a public search. The default mock serves the requested zero-based page from `publicItems`. Use
`searchItemsSearched` to supply a custom page. Generate the graph with
`dexcli visualize examples/top-story/item-search-flow.ts --schema-version 2.0 --lint app --json --open=false --out /tmp/hackernews-typescript-item-search`.
[Item search Flow](examples/top-story/item-search-flow.ts)

Run `npm run check` for package tests, type checking, lint, and build. Conformance fixtures replay Go-recorded provider exchanges in both TypeScript and Python. The example integrations need a running `dexcli dev` stack. `searchStories` ran live against the Hacker News Search API through a real Dex Worker and Client; the feed and item reads have not been verified live from TypeScript.
