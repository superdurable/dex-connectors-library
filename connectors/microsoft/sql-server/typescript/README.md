# Microsoft SQL Server TypeScript connector

`@superdurable/connector-microsoft-sql-server` implements the SQL Server `query` and `execute`
operations for Dex. The runnable [query Flow](examples/query-rows/README.md) is the application
entry point. Both operations use a fresh SQL authentication connection and a transaction. A query
rolls back; an execute commits only after validating its result and row limit.

The result mapping follows the [Go connector](../README.md): `BIGINT` is decimal text, binary
values are base64, `UNIQUEIDENTIFIER` is lowercase, and supported date and time values use Go's
canonical strings with up to seven fractional digits. `REAL` uses the shortest decimal value that
round trips to the SQL float32 value. `DECIMAL` and `NUMERIC` with precision at most 15 return
exact text with the declared scale.

## Known TypeScript limitation

Tedious decodes `DECIMAL` and `NUMERIC` above precision 15, `MONEY`, and `SMALLMONEY` through
JavaScript numbers and discards the stored offset of `DATETIMEOFFSET`. Those values cannot be
reconstructed exactly. The connector inspects column metadata before emitting any rows and takes
the terminal `invalidResponse` branch with the column name and SQL type for these columns, even
when the result set is empty. An `execute` rolls back its transaction on that branch. Cast such
columns to a supported exact representation in SQL when needed, for example `varchar` for a
high precision decimal or `datetimeoffset` value.

Run `npm run check` for unit tests, typecheck, and lint. CI runs `npm run test:integration` against
its SQL Server service container. Real SQL Server and Dex Worker execution were not verified
locally.
