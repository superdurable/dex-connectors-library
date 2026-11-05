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
    # "SELECT id, plan FROM subscriptions WHERE account_email = ?". Values belong in Parameters: the
    # connector never formats them into SQL text, and it cannot detect a statement built by
    # concatenating untrusted input. Only SELECT, WITH, TABLE, VALUES, SHOW, EXPLAIN, DESCRIBE, and
    # DESC statements are accepted, because MySQL commits implicitly before DDL and would run it
    # outside the read-only transaction.
    statement: str = ''
    # Parameters binds the ? placeholders in order. Each value is nil for SQL NULL, a string, bool,
    # integer, float, json.Number, json.RawMessage, []byte for binary data, or time.Time, and is sent
    # as a typed prepared-statement parameter. Pass exact decimals and integers beyond 2^53 as strings
    # or json.Number.
    parameters: tuple[Any, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(QueryRowsInput, '__module__', 'dex_connectors.mysql.models')


# Column describes one result column.
@dataclass(frozen=True, slots=True)
class Column:
    # Name is the column name the server reports, such as the alias given with AS.
    name: str = ''
    # TypeName is the server's column type, such as DECIMAL, UNSIGNED BIGINT, VARCHAR, BLOB, JSON, or TIMESTAMP.
    # MariaDB reports a JSON column as LONGTEXT and an expression's type may differ from a table column's.
    type_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'typeName'})


setattr(Column, '__module__', 'dex_connectors.mysql.models')


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
setattr(RowSet, '__module__', 'dex_connectors.mysql.models')


# ExecuteStatementInput is one parameterized write statement.
@dataclass(frozen=True, slots=True)
class ExecuteStatementInput:
    # Statement is one SQL statement written as a constant in application code, such as
    # "INSERT INTO refunds (order_id, amount) VALUES (?, ?)". Values belong in Parameters: the
    # connector never formats them into SQL text, and it cannot detect a statement built by
    # concatenating untrusted input. Only INSERT, UPDATE, DELETE, REPLACE, and WITH statements are
    # accepted, because DDL, LOCK TABLES, and similar statements commit implicitly and would escape
    # the connector's transaction. A MariaDB RETURNING clause returns rows in the Result.
    statement: str = ''
    # Parameters binds the ? placeholders in order, with the same value rules as QueryRowsInput.
    parameters: tuple[Any, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # IdempotencyKeyPlaceholder, when non-zero, is the position n of the nth ? placeholder, which
    # receives this Step execution's idempotency key. It must equal len(Parameters)+1, so the key is
    # always the last placeholder. The key is a 36-character UUID that stays the same across every
    # retry and Worker replacement of one Step execution, and it is also returned in the Result's
    # Receipt.
    idempotency_key_placeholder: int = _dataclass_field(default=0, metadata={'dex_json_name': 'idempotencyKeyPlaceholder', 'dex_json_omit_default': True})
    # MaxRowsAffected, when set, is the most rows the statement may affect. A statement that affects
    # more is rolled back and selects limitExceeded, which guards against a missing WHERE clause.
    # Nil applies no limit; zero allows only a statement that changes nothing.
    max_rows_affected: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'maxRowsAffected', 'dex_json_omit_default': True})


setattr(ExecuteStatementInput, '__module__', 'dex_connectors.mysql.models')


# StatementExecution is the committed outcome of one ExecuteStatement transaction.
@dataclass(frozen=True, slots=True)
class StatementExecution:
    # RowsAffected is ROW_COUNT() after the statement: rows inserted, deleted, or changed. An UPDATE
    # counts only rows whose values changed. An INSERT ... ON DUPLICATE KEY UPDATE counts 1 for an
    # inserted row, 2 for a changed existing row, and 0 for an existing row left unchanged. For a
    # MariaDB RETURNING statement it is the number of returned rows.
    rows_affected: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rowsAffected'})
    # LastInsertID is LAST_INSERT_ID() after the statement as decimal text: the first AUTO_INCREMENT
    # value the statement inserted, the value passed to LAST_INSERT_ID(expr) if the statement called
    # it, or "0". The connector's session is new, so no earlier statement contributes to it.
    last_insert_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastInsertId'})
    # WarningCount is the number of warnings and notes the statement raised, such as a decimal
    # rounded to its column's scale.
    warning_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'warningCount'})
    # Columns lists MariaDB RETURNING columns in statement order; it is empty without RETURNING.
    columns: tuple[Column, ...] = ()
    # Rows holds MariaDB RETURNING rows with the same type mapping as RowSet.Rows; it is empty,
    # never nil, without RETURNING.
    rows: tuple[dict[str, Any] | None, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'columns', _replace_nil_slices(self.columns, 1))
        object.__setattr__(self, 'rows', _replace_nil_slices(self.rows, 1))


StatementExecution.__annotations__['columns'] = tuple[Column, ...] | None
StatementExecution.__dataclass_fields__['columns'].type = tuple[Column, ...] | None
StatementExecution.__annotations__['rows'] = tuple[dict[str, Any] | None, ...] | None
StatementExecution.__dataclass_fields__['rows'].type = tuple[dict[str, Any] | None, ...] | None
setattr(StatementExecution, '__module__', 'dex_connectors.mysql.models')
