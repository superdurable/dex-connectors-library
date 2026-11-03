# PostgreSQL Python connector

`dex-connector-postgresql` exposes `query` and `execute` Steps for parameterized PostgreSQL statements. Configure a named PostgreSQL connection in Dex Web, then use the same name when constructing the Worker and Step factories. The provider client uses one native Psycopg connection and one transaction per invocation; passwords are resolved for each call.

The Python package follows the Go connector's branch and retry contract. `query` runs read-only and may return `truncated`; `execute` commits one write, can roll back on row limits, and reports `uncertain` only when COMMIT may have reached PostgreSQL. See the [connector README](../README.md) for type mapping, TLS choices, and setup guidance.

Every statement uses PostgreSQL's prepared protocol, including statements with no parameters. PostgreSQL rejects multiple statements in one call. A placeholder count mismatch selects `defect` without retrying. Each invocation has a client deadline of `connectTimeout + statementTimeout + 5` seconds; a timeout after COMMIT is sent selects `uncertain`.

`verify-full` trusts the system root store unless `PGSSLROOTCERT` names a private CA file. `verify-ca` requires `PGSSLROOTCERT` on the Worker because libpq cannot use its system root store without hostname verification. A missing or invalid root certificate selects a terminal authentication failure.

JSON and JSONB values are returned as JSON values when Python can encode every number without changing its text. A number such as `1.10`, a high-precision decimal, or `1e400` selects `invalidResponse` and names the column; the Python SDK JSON codec cannot carry PostgreSQL's raw JSON text. Other scalar mappings follow the Go connector, including unbounded timestamp text and trimmed fractional seconds.

The checked-in `examples/record_refund` Flow exercises both operations with the generated mock connection. Real PostgreSQL integration tests run in CI's service container using `POSTGRESQL_CONNECTOR_TEST_*` variables. The Flow still covers the public operation path; the protocol and value regressions are checked through the direct provider integration test.
