# PostgreSQL Python connector

`dex-connector-postgresql` exposes `query` and `execute` Steps for parameterized PostgreSQL statements. Configure a named PostgreSQL connection in Dex Web, then use the same name when constructing the Worker and Step factories. The provider client uses one native Psycopg connection and one transaction per invocation; passwords are resolved for each call.

The Python package follows the Go connector's branch and retry contract. `query` runs read-only and may return `truncated`; `execute` commits one write, can roll back on row limits, and reports `uncertain` only when COMMIT may have reached PostgreSQL. See the [connector README](../README.md) for type mapping, TLS choices, and setup guidance.

The checked-in `examples/record_refund` Flow exercises both operations with the generated mock connection. Real PostgreSQL integration tests run in CI's service container using `POSTGRESQL_CONNECTOR_TEST_*` variables. Live provider behavior for this Python port has not yet been verified.
