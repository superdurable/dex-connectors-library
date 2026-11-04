# Gemini connector for Python

Install with `pip install dex-connector-google-gemini==0.1.0`. The package
requires `dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, is imported as
`dex_connectors.google_gemini`, and implements the manifest Query
`generateContent` with the same branches, defaults, request checks, and failure
classification as the Go module.

## Operation

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `generateContent` | `new_generate_content_step` | `generated` | `truncated`, `blocked`, `providerRejected`, `invalidResponse`, `defect` |

`generateContent` posts to `<endpoint>/models/<model>:generateContent`. A blank
request model uses the connection's model, and a `models/` prefix is accepted.
A response JSON Schema implies `application/json`. The first candidate's
non-thought text selects `generated` on `STOP`, `truncated` on `MAX_TOKENS`, and
`blocked` on a prompt block reason or a content-policy finish reason.

Generation is billed, so the operation uses sync durability, a 300-second
Execute and heartbeat timeout, and at most three attempts within ten minutes.
HTTP 408, 429, and 5xx other than 501 retry, honoring Google's `RetryInfo`
delay or `Retry-After` up to one hour; other statuses select
`providerRejected`, with authentication for 401 or `API_KEY_INVALID`.

`configuration_ui` accepts the `modelPicker` unit (`UI_UNIT_MODEL_PICKER`),
whose `model` port binds to the request's `/model`.

## Authorization

Create a Dex Web connection named `gemini-api` as described in the
[connector README](../README.md#connection). The API key travels only in the
`x-goog-api-key` header and is resolved for every attempt.

## Example

[`examples/generate_summary`](examples/generate_summary/flow.py) asks Gemini
for a structured JSON summary, honoring the Step's model pick. Run the Worker
with `uv run python -m examples.generate_summary.worker` after `dexcli dev`
has loaded a project containing the `gemini-api` connection.

## Tests

`geminimock.Mock` scripts the operation and defaults to the manifest's
`launchSummary` answer; `validate_generate_content_mock_request` and
`validate_generate_content_mock_answer` apply the Go mock checks. Run
`uv run pytest tests` for provider parity and manifest contract checks.
The conformance test replays the Go-recorded provider exchanges in
[`conformance/connectors/google/gemini/scenarios.json`](../../../../conformance/connectors/google/gemini/scenarios.json),
including request headers, every branch, Retry, and transport failure.
