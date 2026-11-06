# Google Forms Python connector

The `dex-connector-google-forms` package implements `getForm`,
`listResponses`, and `getResponse` as Dex Query Steps. It uses the Google Forms
connection and authorization guidance in the [connector README](../README.md).
The package requires `dex-connectors-sdk==0.7.0`.

The runnable [response recorder](examples/response_recorder/flow.py) reads a
form, one page of responses, and one response by ID. Its
[Worker entrypoint](examples/response_recorder/worker.py) loads the static
`google-forms-intake` connection from Dex project configuration. Configure that
connection in Dex Web, then start the Worker with:

```bash
cd connectors/google/forms/python
uv run python -m examples.response_recorder.worker
```

From this repository, verify Go-recorded provider behavior and Python package
checks with:

```bash
make test-connector-conformance CONNECTOR_DIRECTORY=connectors/google/forms
uv run --project connectors/google/forms/python ruff check connectors/google/forms/python
uv run --project connectors/google/forms/python mypy --strict connectors/google/forms/python/src
uv run --project connectors/google/forms/python pytest connectors/google/forms/python/tests
```

The [Dex integration test](examples/response_recorder/test_integration.py)
starts a real Worker and Flow against a local Dex service and an in-process
Forms stand-in. With `dexcli dev` listening on port 39650, run:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:39650 \
  uv run --project connectors/google/forms/python pytest \
  connectors/google/forms/python/examples/response_recorder/test_integration.py
```

The connector preserves RFC 3339 timestamps through nine fractional digits in
provider results, response filters, and Dex codec round trips. The local
integration test does not exercise live Google OAuth consent, form access, or
Forms API responses; those require a user-controlled Google connection.
