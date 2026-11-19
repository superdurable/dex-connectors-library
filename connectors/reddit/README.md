# Reddit connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/reddit/go@v0.3.0`.

The Go package reads posts, comments, public users, and communities through Reddit's OAuth Data API with nine Queries. The [TypeScript](typescript/README.md) package implements the original three post Queries, and the [Python](python/README.md) package implements `getPost` only.

| Factory | Input | Output | Required branch | Optional branches |
| --- | --- | --- | --- | --- |
| `NewGetPostStep` | `GetPostInput` | `Post` | `found` | `missing`, `providerRejected`, `invalidResponse`, `defect` |
| `NewListSubredditPostsStep` | `ListSubredditPostsInput` | `PostPage` | `listed` | `subredditUnavailable`, `providerRejected`, `invalidResponse`, `defect` |
| `NewSearchPostsStep` | `SearchPostsInput` | `PostPage` | `searched` | `subredditUnavailable`, `providerRejected`, `invalidResponse`, `defect` |
| `NewGetUserStep` | `GetUserInput` | `UserProfile` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `NewGetSubredditStep` | `GetSubredditInput` | `Subreddit` | `found` | `unavailable`, `providerRejected`, `invalidResponse`, `defect` |
| `NewGetSubredditRulesStep` | `GetSubredditRulesInput` | `SubredditRules` | `found` | `unavailable`, `providerRejected`, `invalidResponse`, `defect` |
| `NewSearchSubredditsStep` | `SearchSubredditsInput` | `SubredditPage` | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `NewListSubredditsStep` | `ListSubredditsInput` | `SubredditPage` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `NewListPostCommentsStep` | `ListPostCommentsInput` | `CommentPage` | `listed` | `postUnavailable`, `providerRejected`, `invalidResponse`, `defect` |

The six new Go Queries use the existing `read` OAuth scope. `getUser` reads
public account karma, `getSubreddit` reads public community details,
`getSubredditRules` reads public posting and commenting rules,
`searchSubreddits` and `listSubreddits` return cursor-paginated pages, and `listPostComments`
returns a bounded page of top-level comments. Each is safe to repeat after a
temporary failure. The [topic posts example](go/examples/topic-posts/README.md)
can read community details when `readCommunity` is true.

`getPost` accepts a full `t3_` post ID and returns ID, title, subreddit, relative permalink, and score through `GET /api/info`. An empty listing selects `missing`.

`listSubredditPosts` reads one page of `GET /r/{subreddit}/{sort}` for one community, such as `golang`, or up to 10 joined with `+`, such as `golang+programming`. Names omit `r/`. `sort` is `new` (blank), `hot`, `top`, or `rising`. Only `top` accepts `timeWindow`: `hour`, `day` (blank), `week`, `month`, `year`, or `all`.

`searchPosts` reads one page of post results from `GET /search`. When `subreddit` names one community, it reads `GET /r/{subreddit}/search` with `restrict_sr` instead. `query` is at most 512 characters and is sent unchanged. `sort` is `new` (blank), `relevance`, `top`, `hot`, or `comments`. `timeWindow` takes the same values, and blank searches all time.

Both page operations accept `limit` from 1 through 100, where blank or 0 reads 25, and `after`, the previous page's cursor. A `PostPage` holds `posts` and `after`, which is blank on the last page. A post can appear on two pages when the listing moves between reads, so deduplicate by `id`. Each `ListedPost` carries:

- `id`, the full `t3_` ID that `getPost` also accepts, plus `title`, `subreddit`, and `author`;
- `permalink`, an absolute `https://www.reddit.com` URL;
- `linkUrl` for a link post, with a crosspost's Reddit path joined to the same origin; it is omitted for self posts and for values that are not HTTP or HTTPS;
- `isSelf`, and `selfText`, the author's Markdown as plain text without HTML, cut at a UTF-8 boundary at `MaxSelfTextBytes` (4 KiB) with `isSelfTextTruncated` set;
- `score`, `commentCount`, and `createdAt` in RFC 3339 UTC, to the second;
- `isOver18`, `isSpoiler`, and `isStickied`.

`subredditUnavailable` follows Reddit's answers for a community the account cannot read: a redirect to subreddit search for an unknown name, 404 for a banned one, and 403 with a `private`, `quarantined`, `gated`, `gold_only`, or `banned` reason. A site-wide search never selects it. Any other answer except 200 selects `providerRejected`. Malformed or oversized JSON, a non-post item, and a bad cursor select `invalidResponse`. Invalid input selects `defect` before a request is sent.

HTTP 408, 429, and 5xx responses and transport failures return Retry. A 429 waits for `Retry-After`. When Reddit sends no `Retry-After`, it waits for `X-Ratelimit-Reset`, Reddit's seconds until its rate-limit window resets, rounded up and capped at one hour. This applies to all nine Go Queries and the three TypeScript Queries. The Python `getPost` still reads only `Retry-After`.

Reddit requires [Data API approval](https://support.reddithelp.com/hc/en-us/articles/16160319875092-Reddit-Data-API-Wiki). After approval, create a web app at [Reddit app preferences](https://www.reddit.com/prefs/apps), register the exact Dex Web redirect URI, and configure its client ID and secret in the `reddit` connection. Enter your Reddit username without `/u/`. Dex requests permanent consent for `read`, which covers public posts, comments, users, and subreddits, so these Queries need no new scope and existing connections keep working. Dex refreshes OAuth tokens and sends Reddit's required app and username User-Agent. The remaining URL and request limit fields have safe defaults in Dex Web; the response limit fits a full page of 100 posts.

The results are public metadata and user content, so applications storing titles, text, or other user content must honor Reddit's deletion and retention requirements. The [post example](go/examples/post/README.md), [topic posts example](go/examples/topic-posts/README.md), [TypeScript topic posts example](typescript/examples/topic-posts/README.md), [Python example](python/examples/post/README.md), and [TypeScript example](typescript/examples/post/README.md) use a static `reddit` connection. The generated `redditmock` package answers `listSubredditPosts`, `searchPosts`, `searchSubreddits`, and `listSubreddits` with three chained pages each. The Go-recorded [conformance scenarios](../../conformance/connectors/reddit/scenarios.json) cover every branch and Retry of all nine Go Queries; TypeScript replays its three post Queries and Python replays `getPost`.

The manifest reviews all nine Queries for agent use. TypeScript offers its three post Queries and Python offers `getPost` as read-only tools. The Go module generates no agent tools until it requires `sdkgo` v0.35.0 or later.

From `connectors/reddit/go`, run `GOWORK=off go test -race ./...` and `GOWORK=off go vet ./...`. With `dexcli dev` running, run `GOWORK=off go test -p 1 -tags=integration ./... -count=1`. The topic posts example ran on a real Dex stack against a local stand-in for Reddit, loaded through `dexcli dev --connector-release-override`. Live Reddit authorization and provider behavior have not been verified in this checkout, including the redirect, 404, and 403 answers behind `subredditUnavailable`, `+`-joined listings, the `restrict_sr` search, and the `X-Ratelimit-Reset` value on a 429.
