# Python OAuth HTTP API request example

The `OAuth2HTTPPythonAPIRequest` Flow sends one request using the static
`oauth2-http-api` connection. Configure its OAuth server, API origin, and client
credentials in Dex Web Connections as described in the connector README.

Run `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, then run
from the Python package directory:

```sh
uv run python -m examples.api_request.worker
uv run python -m examples.api_request.start oauth2-http-python-1 examples/api_request/input.example.json
```

The integration test exercises a fake provider through a real Dex Worker and
Client. Live provider authorization remains unverified.
