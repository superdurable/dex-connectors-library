# Figma connector for Python

Install with `pip install dex-connector-figma==0.2.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `getFile`, `getFileNodes`, and `getFileComments` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `getFile` | `new_get_file_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getFileComments` | `new_get_file_comments_step` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getFileNodes` | `new_get_file_nodes_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`getFileNodes` takes one to 100 node IDs such as `1:2` and returns their document subtrees.
All three queries take a file key of 1 to 128 letters, digits, `_`, or `-`. `getFile`
returns the file name, last-modified text, and the document node tree as
JSON. Integral number values retain their exact integer value; a number that
Dex JSON encoding would round selects `invalidResponse` and names its document
field. A response without a name or document also selects `invalidResponse`.
404 selects `notFound`; 408, 409, 429, 5xx, and transport failures retry; 401,
403, and other non-200 responses select `providerRejected`.

## Authorization

Create a Dex Web connection named `figma` as described in the
[connector README](../README.md#connection). The personal access token travels
only in `X-Figma-Token` and is resolved for every attempt.

## Example

[`examples/figma_lookup`](examples/figma_lookup/flow.py) reads a file and then
its comments. Run the Worker with
`uv run python -m examples.figma_lookup.worker` after `dexcli dev` has loaded a
project containing the `figma` connection.

## Tests

`figmamock.Mock` scripts all three Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
