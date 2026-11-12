# Postmark connector for Python

Install with `pip install dex-connector-postmark==0.3.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `listOutboundMessages` and `getMessageDetails` with the same
branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listOutboundMessages` | `new_list_outbound_messages_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `getMessageDetails` | `new_get_message_details_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`listOutboundMessages` sends `count` (zero or omitted means 50, at most 500) and
`offset` (zero or omitted starts at the first message);
`count + offset` must stay within Postmark's 10,000-message search window.
`getMessageDetails` selects `notFound` for HTTP 404 or Postmark error code 701.
408, 429, 5xx, and transport failures retry; other 4xx responses select
`providerRejected`.

## Authorization

Create a Dex Web connection named `postmark` as described in the
[connector README](../README.md#authorization). The Server API token travels
only in the `X-Postmark-Server-Token` header and is resolved for every attempt.

## Example

[`examples/outbound_lookup`](examples/outbound_lookup/flow.py) checks the page
window, lists outbound messages with `new_list_outbound_messages_step`, and
records the page. Run the Worker with
`uv run python -m examples.outbound_lookup.worker` after `dexcli dev` has
loaded a project containing the `postmark` connection.

## Tests

`postmarkmock.Mock` scripts both Queries and defaults to the manifest mock
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
