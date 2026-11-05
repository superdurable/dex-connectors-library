# Snowflake Python connector

The Python package `dex-connector-snowflake` provides the Snowflake SQL API operations `submitStatement`, `getStatementResult`, and `cancelStatement` for Dex Flows. It uses the manifest's key-pair or programmatic access token credential method and resolves the credential on every provider call. A submission returns a statement handle; the application reads its status and one bounded result partition after a durable wait.

See [the connector README](../README.md) for Snowflake setup, statement parameter types, result conversion, and connection configuration. The runnable application is in [`examples/account_usage_summary`](examples/account_usage_summary).

The Python Dex JSON codec cannot emit raw JSON tokens or `Decimal` values. For `variant`, `object`, `array`, and `map` columns, the package decodes JSON numbers to binary floating point before Dex encodes the result. Large integers and high-precision decimals may round; use a Snowflake SQL cast to text when exact digits matter. Duplicate object keys and numbers outside the codec's finite range select `invalidResponse`. The Go package can preserve original JSON text through `json.RawMessage`.
