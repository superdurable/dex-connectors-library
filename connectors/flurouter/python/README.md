# FluRouter Python connector

`dex-connector-flurouter` provides the `listModels` Query. It returns model IDs visible to the connected API Key; the catalog can differ from the public model list. The secret Key stays in connection credentials and is sent only in the HTTPS Bearer header.

## Connection

Sign in from the [FluRouter guide](https://flurouter.net/guide), open **Console → API Keys → Create API Key**, choose a name and model permissions, and copy the one-time `sk-il-` Key into secret `apiKey` in Dex Connections. A redemption code is not an API Key; blank prevents requests. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. Start the Query with empty JSON `{}`.

Optional `providerRejected`, `invalidResponse`, and `defect` outcomes fail the Flow unless the application wires a reaction. Temporary HTTP and transport failures retry under the Step policy. See `examples/model_catalog/` for a runnable Flow, Worker, real Dex integration test, and strict FDG instructions. `uv run pytest tests` replays Go-recorded scenarios. Live FluRouter authorization remains unverified.
