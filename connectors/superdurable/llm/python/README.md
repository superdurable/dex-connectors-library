# Super Durable LLM Python connector

`dex-connector-superdurable-llm` exposes the provider-neutral `generateText`
Dex Query for OpenAI, Claude, Gemini, Qwen, DeepSeek, Meta, Mistral, Kimi,
xAI, and OpenAI-compatible gateways. Its public request and response types
reuse the released Connector SDK text generation contract. The connection's
provider chooses the HTTP dialect and model default. See the
[connector README](../README.md#connection) for provider keys, regions,
model selection, and every connection field.

Install from this directory with `uv sync --locked`. The package pins
`dex-connectors-sdk==0.3.0`. `new_project_connection` reads the static
connection name from the Dex project and resolves the API key at each call.
The key stays out of Flow input and persistence.

The runnable [summary example](examples/summarize_text/README.md) includes a
Worker, Client start command, sample input, and a real Dex integration test.
Only `generated` is wired. `truncated`, `blocked`, `providerRejected`,
`invalidResponse`, and `defect` fail the Flow if selected. Safe transport,
rate-limit, and provider availability failures retry under the manifest's
three-attempt, one-hour billed-operation budget.

For an OpenAI-compatible gateway, a public HTTPS base URL uses the SDK's
`publicHTTPS` address policy. Only a loopback IP literal or `localhost`
uses an unrestricted transport; `localhost` must resolve exclusively to
loopback addresses before a credentialed request. Literal private, shared,
link-local, and other non-public IP hosts fail at construction. Local gateway
requests use the same 870-second exchange limit as other providers. The connector never
rewrites the configured URL. Python's Dex JSON codec cannot preserve
`Decimal` or raw JSON number precision; requests containing those values
fail terminally before a provider call.

Run package verification with:

```sh
uv run pytest -q tests
uv run ruff check src/dex_connectors/superdurable_llm/*.py tests/*.py examples/summarize_text/*.py
uv run mypy src/dex_connectors/superdurable_llm
```

The Go-recorded HTTP scenarios in
[`conformance/connectors/superdurable/llm/scenarios.json`](../../../../conformance/connectors/superdurable/llm/scenarios.json)
replay in `tests/test_conformance.py`. Provider replay and the local Dex test
do not establish live authorization or output behavior for a real provider.
