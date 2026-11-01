# Unsplash connector for Python

Install with `pip install dex-connector-unsplash==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `searchPhotos` and `getPhoto` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `searchPhotos` | `new_search_photos_step` | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `getPhoto` | `new_get_photo_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`searchPhotos` requires a phrase and accepts `per_page` up to 30. Every photo
must carry an ID, a photographer name, and HTTPS page, image, and photographer
links, or the response is `invalidResponse`. `getPhoto` maps 404 to `notFound`.
408, 429, 5xx, and transport failures retry; 401 and 403 select
`providerRejected` with authentication and authorization kinds.

## Authorization

Create a Dex Web connection named `unsplash` as described in the
[connector README](../README.md#authorization). The access key travels only in
the `Authorization: Client-ID` header and is resolved for every attempt.

## Example

[`examples/photo_search`](examples/photo_search/flow.py) searches photos by
phrase and records the page. Run the Worker with
`uv run python -m examples.photo_search.worker` after `dexcli dev` has loaded a
project containing the `unsplash` connection.

## Tests

`unsplashmock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
