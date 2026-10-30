# TMDB connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/tmdb/go@v0.2.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/tmdb`.

This Dex connector calls the public TMDB JSON API. Its
manifest version is `v0.1.0`; the Go module requires connector SDK `v0.33.0`
    and Dex Go SDK `v1.5.0` or newer. The runnable
[`movie-search` example](go/examples/movie-search/README.md) shows a
complete Worker and the generated operation-specific Step factory.

## Operations

| Operation | Kind | Output |
| --- | --- | --- |
| `searchMovies` | query | MoviePage |
| `getMovie` | query | Movie |

This connector is read-only and uses the API Read Access Token in an Authorization header, never a URL parameter.

## Authorization

Start at [https://www.themoviedb.org/settings/api](https://www.themoviedb.org/settings/api) and follow
**Account Settings > API > API Read Access Token**. Request API access in account settings, then copy the API Read Access Token. It is a secret opaque string; blank prevents requests. Paste the token in the
secret `api_key` field in Dex Web Connections.
The key travels only in `Authorization`. Leave `baseUrl`, `timeout`,
and `maxResponseBytes` blank for the displayed public endpoint and safety
defaults; override the base URL only for a compatible gateway or test server.

## Failures and retries

| Condition | Result |
| --- | --- |
| Successful documented response | operation's happy branch |
| Known missing resource on a detail operation | `notFound` |
| Other definite 4xx rejection | `providerRejected` |
| Malformed or oversized successful JSON | `invalidResponse` |
| Invalid local input or credentials | `defect` |
| HTTP 408, 429, 5xx, transport interruption | Retry until the default budget ends, then Flow failure |

Every non-happy branch is optional. The example wires only the happy branch;
an unwired optional outcome fails the Flow. Provider error response text and
credentials never enter a Dex result, receipt, Stream, or log.

## Mocks

`tmdbmock.New(t, "mock")` binds the default manifest answers and the
generated `zz_generated_mock_test.go` strictly decodes every fixture. The
checked-in [`workflow_mock_integration_test.go`](go/examples/movie-search/flow/workflow_mock_integration_test.go)
uses the same generated mock against a real Dex Worker in CI. Its start looks
like this runnable source:

```go
mock := tmdbmock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

## Provider documentation and verification

- [https://developer.themoviedb.org/reference/search-movie](https://developer.themoviedb.org/reference/search-movie)
- [https://developer.themoviedb.org/reference/movie-details](https://developer.themoviedb.org/reference/movie-details)
- [https://developer.themoviedb.org/docs/authentication-application](https://developer.themoviedb.org/docs/authentication-application)

The HTTP unit tests verify request shapes and local classification against
an `httptest` server. Provider authorization, live API response variations,
and real Dex integration remain unverified locally. CI runs the mock backed
real Dex test; a user-controlled live connection is still needed to confirm
the provider account and permissions.
