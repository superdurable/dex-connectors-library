# TMDB connector for Python

Install with `pip install dex-connector-tmdb==0.2.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `searchMovies` and `getMovie` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `searchMovies` | `new_search_movies_step` | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `getMovie` | `new_get_movie_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`searchMovies` requires non-blank `query` text; `page` zero means TMDB's first
page and may be at most 500. `getMovie` requires a positive movie ID, rejects a
response for another ID as `invalidResponse`, and selects `notFound` for HTTP
404. 408, 429, 5xx, and transport failures retry; other non-200 responses
select `providerRejected`.

## Authorization

Create a Dex Web connection named `tmdb` as described in the
[connector README](../README.md#authorization). The API Read Access Token
travels only in the `Authorization: Bearer` header and is resolved for every
attempt.

## Example

[`examples/movie_search`](examples/movie_search/flow.py) checks the search
text, searches with `new_search_movies_step`, and records the page. Run the
Worker with `uv run python -m examples.movie_search.worker` after `dexcli dev`
has loaded a project containing the `tmdb` connection.

## Tests

`tmdbmock.Mock` scripts both Queries and defaults to the manifest mock answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
