# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# QueryRowsInput is one parameterized read statement.
@dataclass(frozen=True, slots=True)
class QueryRowsInput:
    # Statement is one SQL statement written as a constant in application code, such as
    # "SELECT id, plan FROM dbo.subscriptions WHERE account_email = @p1". Values belong in
    # Parameters: the connector never formats them into SQL text, and it cannot detect a statement
    # built by concatenating untrusted input. Only statements that start with SELECT or WITH are
    # accepted, and the statement must return one result set.
    statement: str = ''
    # Parameters binds @p1 through @pN in order. Each value is nil for SQL NULL, a string, bool,
    # integer, float, json.Number, json.RawMessage, []byte for binary data, or time.Time, and is sent
    # as a typed sp_executesql parameter. Pass exact decimals as strings or json.Number and CAST them
    # in the statement when the comparison type matters.
    parameters: tuple[Any, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(QueryRowsInput, '__module__', 'dex_connectors.microsoft_sql_server.models')


# Column describes one result column.
@dataclass(frozen=True, slots=True)
class Column:
    # Name is the column name the server reports, such as the alias given with AS.
    name: str = ''
    # TypeName is the server's column type, such as DECIMAL, BIGINT, NVARCHAR, VARBINARY,
    # UNIQUEIDENTIFIER, or DATETIMEOFFSET. NUMERIC is reported as DECIMAL, and a CLR type such as
    # GEOGRAPHY or HIERARCHYID by its own name.
    type_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'typeName'})


setattr(Column, '__module__', 'dex_connectors.microsoft_sql_server.models')


# RowSet is the bounded result of one QueryRows statement.
@dataclass(frozen=True, slots=True)
class RowSet:
    # Columns lists the result columns in statement order.
    columns: tuple[Column, ...] = ()
    # Rows maps each column name to its JSON value using the connector's documented type mapping.
    # It is empty, never nil, when the statement returned no rows.
    rows: tuple[dict[str, Any] | None, ...] = ()
    # Truncated reports that the statement produced more rows or bytes than maxRows or
    # maxResponseBytes allow, so Rows holds only the rows that fit.
    truncated: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'columns', _replace_nil_slices(self.columns, 1))
        object.__setattr__(self, 'rows', _replace_nil_slices(self.rows, 1))


RowSet.__annotations__['columns'] = tuple[Column, ...] | None
RowSet.__dataclass_fields__['columns'].type = tuple[Column, ...] | None
RowSet.__annotations__['rows'] = tuple[dict[str, Any] | None, ...] | None
RowSet.__dataclass_fields__['rows'].type = tuple[dict[str, Any] | None, ...] | None
setattr(RowSet, '__module__', 'dex_connectors.microsoft_sql_server.models')


# ExecuteStatementInput is one parameterized write statement.
@dataclass(frozen=True, slots=True)
class ExecuteStatementInput:
    # Statement is one SQL statement written as a constant in application code, such as
    # "INSERT INTO dbo.refunds (order_id, amount) VALUES (@p1, CAST(@p2 AS decimal(12, 2)))".
    # Values belong in Parameters: the connector never formats them into SQL text, and it cannot
    # detect a statement built by concatenating untrusted input. Only statements that start with
    # INSERT, UPDATE, DELETE, MERGE, or WITH are accepted. A MERGE must end with a semicolon.
    statement: str = ''
    # Parameters binds @p1 through @pN in order, with the same value rules as QueryRowsInput.
    parameters: tuple[Any, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # IdempotencyKeyPlaceholder, when non-zero, is the number n of the @pn placeholder that receives
    # this Step execution's idempotency key. It must equal len(Parameters)+1, so the key is always
    # the last placeholder. The key is a 36-character lowercase UUID, bound as nvarchar, that stays
    # the same across every retry and Worker replacement of one Step execution; it is also returned
    # in the Result's Receipt.
    idempotency_key_placeholder: int = _dataclass_field(default=0, metadata={'dex_json_name': 'idempotencyKeyPlaceholder', 'dex_json_omit_default': True})
    # MaxRowsAffected, when set, is the most rows the statement may affect. A statement that affects
    # more is rolled back and selects limitExceeded, which guards against a missing WHERE clause.
    # Nil applies no limit; zero allows only a statement that changes nothing.
    max_rows_affected: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'maxRowsAffected', 'dex_json_omit_default': True})
    # ReturnsOutputRows declares that the statement has an OUTPUT clause that returns rows to the
    # client. The connector then returns those rows, bounded by maxRows and maxResponseBytes, and
    # counts them as the rows affected. Leave it false for a statement without OUTPUT, or whose
    # OUTPUT clause only writes INTO a table; OUTPUT rows of such a statement are discarded.
    returns_output_rows: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'returnsOutputRows', 'dex_json_omit_default': True})


setattr(ExecuteStatementInput, '__module__', 'dex_connectors.microsoft_sql_server.models')


# StatementExecution is the committed outcome of one ExecuteStatement transaction.
@dataclass(frozen=True, slots=True)
class StatementExecution:
    # RowsAffected is the number of rows the statement inserted, updated, deleted, or merged. With
    # ReturnsOutputRows it is the number of OUTPUT rows, one per affected row. Otherwise it is the
    # sum of the row counts the server reported, which also includes rows changed by an AFTER
    # trigger that does not SET NOCOUNT ON.
    rows_affected: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rowsAffected'})
    # Columns lists the OUTPUT columns in statement order; it is empty without ReturnsOutputRows.
    columns: tuple[Column, ...] = ()
    # Rows holds the OUTPUT rows with the same type mapping as RowSet.Rows; it is empty, never nil,
    # without ReturnsOutputRows.
    rows: tuple[dict[str, Any] | None, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'columns', _replace_nil_slices(self.columns, 1))
        object.__setattr__(self, 'rows', _replace_nil_slices(self.rows, 1))


StatementExecution.__annotations__['columns'] = tuple[Column, ...] | None
StatementExecution.__dataclass_fields__['columns'].type = tuple[Column, ...] | None
StatementExecution.__annotations__['rows'] = tuple[dict[str, Any] | None, ...] | None
StatementExecution.__dataclass_fields__['rows'].type = tuple[dict[str, Any] | None, ...] | None
setattr(StatementExecution, '__module__', 'dex_connectors.microsoft_sql_server.models')
