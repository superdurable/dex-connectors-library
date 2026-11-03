# Snowflake Python connector

The Python package `dex-connector-snowflake` provides the Snowflake SQL API operations `submitStatement`, `getStatementResult`, and `cancelStatement` for Dex Flows. It uses the manifest's key-pair or programmatic access token credential method and resolves the credential on every provider call. A submission returns a statement handle; the application reads its status and one bounded result partition after a durable wait.

See [the connector README](../README.md) for Snowflake setup, statement parameter types, result conversion, and connection configuration. The runnable application is in [`examples/account_usage_summary`](examples/account_usage_summary).
