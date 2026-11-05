# Snowflake Python connector

The Python package `dex-connector-snowflake` provides the Snowflake SQL API operations `submitStatement`, `getStatementResult`, and `cancelStatement` for Dex Flows. It uses the manifest's key-pair or programmatic access token credential method and resolves the credential on every provider call. A submission returns a statement handle; the application reads its status and one bounded result partition after a durable wait.

See [the connector README](../README.md) for Snowflake setup, statement parameter types, result conversion, and connection configuration. The runnable application is in [`examples/account_usage_summary`](examples/account_usage_summary).

The Python Dex JSON codec cannot emit raw JSON tokens or `Decimal` values. For `variant`, `object`, `array`, and `map` columns, the Python package returns exactly representable JSON numbers and rejects fractional values whose precision or text would change, as well as duplicate object keys. Those rows produce an `invalidResponse` outcome instead of a rounded value. The Go package can return such values through `json.RawMessage`.
