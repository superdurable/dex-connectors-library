# Bitbucket connector for Python

Install with `pip install dex-connector-atlassian-bitbucket==0.1.0`. The package
requires `dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and
implements the manifest Queries `listRepositories` and `listPullRequests` with
the same branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listRepositories` | `new_list_repositories_step` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `listPullRequests` | `new_list_pull_requests_step` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Both read Bitbucket's first page; `workspace` and `repository` must each be one
path segment. The page keeps Bitbucket's `next` URL, `page`, and `pagelen`.
HTTP 404 selects `notFound`; 408, 429, 5xx, and transport failures retry; 401,
403, and other non-200 responses select `providerRejected`.

## Authorization

Create a Dex Web connection named `bitbucket` as described in the
[connector README](../README.md#authentication-and-configuration). The access
token travels only in the `Authorization: Bearer` header and is resolved for
every attempt.

## Example

[`examples/resource_read`](examples/resource_read/flow.py) reads a workspace's
repositories and records the page. Run the Worker with
`uv run python -m examples.resource_read.worker` after `dexcli dev` has loaded
a project containing the `bitbucket` connection.

## Tests

`bitbucketmock.Mock` scripts both Queries and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
