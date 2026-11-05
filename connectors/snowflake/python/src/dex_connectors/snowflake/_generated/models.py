# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SubmitStatementInput is one parameterized SQL statement.
@dataclass(frozen=True, slots=True)
class SubmitStatementInput:
    # Statement is one SQL statement written as a constant in application code, such as
    # "SELECT COUNT(*) AS EVENT_COUNT FROM USAGE_EVENTS WHERE ACCOUNT_ID = ?". Values belong in
    # Parameters: the connector never formats them into SQL text, and it cannot detect a statement
    # built by concatenating untrusted input. Snowflake runs exactly one statement per request, so
    # several statements, BEGIN, COMMIT, USE, and ALTER SESSION are rejected by Snowflake.
    statement: str = ''
    # Parameters binds each ? placeholder in order, outside string literals, quoted identifiers, and
    # comments. Each value is nil for SQL NULL, a string (TEXT), bool (BOOLEAN), integer (FIXED),
    # finite float (REAL), json.Number (FIXED for integers, otherwise TEXT), json.RawMessage (TEXT;
    # wrap the placeholder in PARSE_JSON), []byte (BINARY), or time.Time (TIMESTAMP_TZ). Cast a
    # placeholder when the type matters, such as ?::NUMBER(12,2).
    parameters: tuple[Any, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(SubmitStatementInput, '__module__', 'dex_connectors.snowflake.models')


# StatementSubmission identifies a statement Snowflake accepted for asynchronous execution.
@dataclass(frozen=True, slots=True)
class StatementSubmission:
    # StatementHandle is Snowflake's UUID-shaped handle, which getStatementResult and cancelStatement take.
    statement_handle: str = _dataclass_field(default='', metadata={'dex_json_name': 'statementHandle'})
    # SubmittedAt is when Snowflake created the statement, when it reported that time.
    submitted_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'submittedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(StatementSubmission, '__module__', 'dex_connectors.snowflake.models')


# Column describes one result column from Snowflake's resultSetMetaData.rowType.
@dataclass(frozen=True, slots=True)
class Column:
    # Name is the column name Snowflake reports, upper case unless the statement quoted an alias.
    name: str = ''
    # Type is the lower-case SQL API type, such as fixed, real, decfloat, text, boolean, binary,
    # date, time, timestamp_ntz, timestamp_ltz, timestamp_tz, variant, object, or array.
    type: str = ''
    # Precision is the number of digits of a fixed column, such as 38 for NUMBER.
    precision: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Scale is the number of fractional digits of a fixed column, or of a time or timestamp column.
    scale: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Length is the maximum length of a text or binary column.
    length: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Nullable reports whether the column can hold SQL NULL.
    nullable: bool = False


setattr(Column, '__module__', 'dex_connectors.snowflake.models')


# GetStatementResultInput identifies a submitted statement and the result partition to read.
@dataclass(frozen=True, slots=True)
class GetStatementResultInput:
    # StatementHandle is the handle submitStatement returned.
    statement_handle: str = _dataclass_field(default='', metadata={'dex_json_name': 'statementHandle'})
    # Partition is the zero-based result partition to read. Partition 0 also carries the column
    # metadata, the partition count, and the total row count; later partitions carry only rows.
    partition: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Columns must hold the Columns of the partition-0 Result when Partition is greater than zero,
    # because Snowflake sends no column metadata with later partitions. It is ignored for partition 0.
    columns: tuple[Column, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(GetStatementResultInput, '__module__', 'dex_connectors.snowflake.models')


# StatementStats counts the rows one DML statement changed.
@dataclass(frozen=True, slots=True)
class StatementStats:
    # RowsInserted is Snowflake's numRowsInserted.
    rows_inserted: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rowsInserted'})
    # RowsUpdated is Snowflake's numRowsUpdated.
    rows_updated: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rowsUpdated'})
    # RowsDeleted is Snowflake's numRowsDeleted.
    rows_deleted: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rowsDeleted'})
    # DuplicateRowsUpdated is Snowflake's numDuplicateRowsUpdated.
    duplicate_rows_updated: int = _dataclass_field(default=0, metadata={'dex_json_name': 'duplicateRowsUpdated'})


setattr(StatementStats, '__module__', 'dex_connectors.snowflake.models')


# StatementResult is a statement's status and, once it finished, one bounded result partition.
@dataclass(frozen=True, slots=True)
class StatementResult:
    # StatementHandle is the statement that was read.
    statement_handle: str = _dataclass_field(default='', metadata={'dex_json_name': 'statementHandle'})
    # Columns lists the result columns in statement order; it is empty while the statement runs.
    columns: tuple[Column, ...] = ()
    # Rows maps each column name to its JSON value using the connector's documented type mapping.
    # It is empty, never nil, while the statement runs or when the partition holds no rows.
    rows: tuple[dict[str, Any] | None, ...] = ()
    # Partition is the partition these rows came from.
    partition: int = 0
    # PartitionCount is the number of result partitions; it is reported only by partition 0.
    partition_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'partitionCount', 'dex_json_omit_default': True})
    # TotalRowCount is the number of rows across every partition; it is reported only by partition 0.
    total_row_count: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'totalRowCount', 'dex_json_omit_default': True})
    # Truncated reports that the partition held more rows or bytes than maxRows or maxResponseBytes
    # allow, so Rows holds only the rows that fit.
    truncated: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})
    # Stats reports the rows a DML statement inserted, updated, or deleted, when Snowflake sends them.
    stats: StatementStats | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # SubmittedAt is when Snowflake created the statement, when it reported that time.
    submitted_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'submittedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'columns', _replace_nil_slices(self.columns, 1))
        object.__setattr__(self, 'rows', _replace_nil_slices(self.rows, 1))


StatementResult.__annotations__['columns'] = tuple[Column, ...] | None
StatementResult.__dataclass_fields__['columns'].type = tuple[Column, ...] | None
StatementResult.__annotations__['rows'] = tuple[dict[str, Any] | None, ...] | None
StatementResult.__dataclass_fields__['rows'].type = tuple[dict[str, Any] | None, ...] | None
setattr(StatementResult, '__module__', 'dex_connectors.snowflake.models')


# CancelStatementInput identifies the statement to cancel.
@dataclass(frozen=True, slots=True)
class CancelStatementInput:
    # StatementHandle is the handle submitStatement returned.
    statement_handle: str = _dataclass_field(default='', metadata={'dex_json_name': 'statementHandle'})


setattr(CancelStatementInput, '__module__', 'dex_connectors.snowflake.models')


# StatementCancellation records that Snowflake accepted a cancellation.
@dataclass(frozen=True, slots=True)
class StatementCancellation:
    # StatementHandle is the statement Snowflake was asked to cancel.
    statement_handle: str = _dataclass_field(default='', metadata={'dex_json_name': 'statementHandle'})


setattr(StatementCancellation, '__module__', 'dex_connectors.snowflake.models')
