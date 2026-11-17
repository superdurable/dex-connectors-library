# Python OAuth HTTP API request example

The `OAuth2HTTPPythonAPIRequest` Flow sends one repeat-safe request with
`sendRequest`, and the `OAuth2HTTPPythonOrder` Flow sends one order POST with
`postNonIdempotent`, which never sends it again after it may have reached the
API. Both use the static `oauth2-http-api` connection. Configure its OAuth
server, API origin, and client credentials in Dex Web Connections as described
in the connector README.

Run `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, then run
from the Python package directory:

```sh
uv run python -m examples.api_request.worker
uv run python -m examples.api_request.start oauth2-http-python-1 examples/api_request/input.example.json
```

Start the order Flow from Dex Web **Start Flow** with an input such as
`{"path": "/orders", "body": "{\"item\":\"gear\"}"}`. Its `PostOrder` Step runs
with sync durability; an answer that does not prove the API refused or ignored
the POST, such as a lost connection or a 502, fails the Flow on the unwired
`uncertain` branch.

Generate and lint the strict Flow Definition Graph 2.0 of each Flow with the
package's Python interpreter:

```sh
dexcli visualize examples/api_request/flow.py --language python --schema-version 2.0 --lint app --json --out - --open=false --python .venv/bin/python
dexcli visualize examples/api_request/order_flow.py --language python --schema-version 2.0 --lint app --json --out - --open=false --python .venv/bin/python
```

The integration tests exercise a fake provider through a real Dex Worker and
Client: a completed request, an order sent once, and an order whose 502 fails
the Flow without a second POST. Live provider authorization remains unverified.
