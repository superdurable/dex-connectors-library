# OpenAI connector for Python

Install with `pip install dex-connector-openai==0.1.0`. The package requires
`dex-connectors-sdk==0.2.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest operations `createResponse` (Mutation) and `retrieveResponse` (Query)
with the same branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `createResponse` | `new_create_response_step` | `completed` | `failed`, `providerRejected`, `uncertain`, `defect` |
| `retrieveResponse` | `new_retrieve_response_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

## createResponse retries

The Responses API does not deduplicate create requests, so `createResponse`
fences every retry with its own Dex heartbeat checkpoint. Keep the Step's
manifest sync Execute durability, and record no other heartbeats in that Step.

- An attempt that observed a response ID reads that response back instead of
  creating another one, polling `queued` and `in_progress` until five seconds
  before the Execute deadline.
- Only an attempt that provably created nothing (503, 429, a connection
  refused before sending, or a `cancelled` response) lets the next attempt
  resend.
- Every other unconfirmed outcome selects `uncertain`: 408, 409, a 5xx other
  than 503, an ambiguous transport failure, a streamed error before a response
  ID, an unreadable body, or a read-back that cannot confirm the response.

The manifest retry policy allows three attempts within five minutes because a
duplicate create is billed.

## Streaming and structured output

Passing `progress_stream` or `text_stream` to `new_create_response_step` makes
OpenAI stream the response. Lifecycle events become progress, text deltas are
appended to the text Stream best effort, and the terminal event selects the
branch. `maxResponseBytes` bounds the whole stream and `maxSseEventBytes` one
event; after either limit the connector drains the stream and reads the stored
response back when its ID was observed.

`StructuredOutput` sends a JSON Schema `text.format`; the completed
`output_text` is then the JSON value.

## Authorization and configuration

Create a Dex Web connection as described in the
[connector README](../README.md). The API key travels only in the
`Authorization` header. The `modelPicker` configuration UI unit
(`UI_UNIT_MODEL_PICKER`, port `UI_MODEL_PICKER_PORT_MODEL`) lets Dex Web store
a per-Step model; a blank model uses the connection's model.

## Example

[`examples/summarize_text`](examples/summarize_text/flow.py) records the
request, creates a summary with streamed text and a model picker, and records
the outcome. Run the Worker with
`uv run python -m examples.summarize_text.worker` after `dexcli dev` has loaded
a project containing the `openai-api` connection.

## Tests

`openaimock.Mock` scripts both operations and defaults to the manifest's
default answers. Run `uv run pytest tests` for fencing, streaming, read-back,
and manifest contract checks. With `dexcli dev` running, run
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:34701 uv run pytest integration` to drive the
example through a real Dex Worker with a scripted provider transport. The
Python mock checks create requests and strict structured answers with the SDK
text generation validator, matching the Go connector's mock contract.

`uv run pytest tests/test_conformance.py` replays the Go-recorded provider
scenarios for both operations. Live OpenAI authorization and provider behavior
have not been verified.
