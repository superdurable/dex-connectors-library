# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Column, ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution
from .definitions import (
    CONNECTOR_ID,
    COMPLETED,
    DEFECT,
    EXECUTE_DEFINITION,
    INVALID_RESPONSE,
    LIMIT_EXCEEDED,
    PROVIDER_REJECTED,
    QUERY_DEFINITION,
    TRUNCATED,
    UNCERTAIN,
)


def query_completed(value: RowSet, failure: Failure | None = None) -> Case[RowSet]:
    return Case.branch_result(COMPLETED, value, failure)


setattr(query_completed, "__module__", "dex_connectors.postgresql.postgresqlmock")


def query_truncated(value: RowSet, failure: Failure | None = None) -> Case[RowSet]:
    return Case.branch_result(TRUNCATED, value, failure)


setattr(query_truncated, "__module__", "dex_connectors.postgresql.postgresqlmock")


def query_provider_rejected(value: RowSet, failure: Failure | None = None) -> Case[RowSet]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


setattr(query_provider_rejected, "__module__", "dex_connectors.postgresql.postgresqlmock")


def query_invalid_response(value: RowSet, failure: Failure | None = None) -> Case[RowSet]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)


setattr(query_invalid_response, "__module__", "dex_connectors.postgresql.postgresqlmock")


def query_defect(value: RowSet, failure: Failure | None = None) -> Case[RowSet]:
    return Case.branch_result(DEFECT, value, failure)


setattr(query_defect, "__module__", "dex_connectors.postgresql.postgresqlmock")


def query_retry(failure: Failure, after_seconds: float = 0) -> Case[RowSet]:
    return Case.retry(failure, after_seconds)


setattr(query_retry, "__module__", "dex_connectors.postgresql.postgresqlmock")


def query_no_recorded_refund() -> Case[RowSet]:
    return Case.branch_result(
        COMPLETED,
        RowSet(
            columns=(
                Column(name="refund_id", type_name="int8", type_oid=20),
                Column(name="order_id", type_name="text", type_oid=25),
                Column(name="amount_usd", type_name="numeric", type_oid=1700),
                Column(name="external_reference", type_name="text", type_oid=25),
                Column(name="idempotency_key", type_name="uuid", type_oid=2950),
                Column(name="recorded_at", type_name="timestamptz", type_oid=1184),
            ),
            rows=(),
        ),
    )


setattr(query_no_recorded_refund, "__module__", "dex_connectors.postgresql.postgresqlmock")


def query_truncated_lookup() -> Case[RowSet]:
    return Case.branch_result(
        TRUNCATED,
        RowSet(
            columns=(
                Column(name="refund_id", type_name="int8", type_oid=20),
                Column(name="order_id", type_name="text", type_oid=25),
                Column(name="amount_usd", type_name="numeric", type_oid=1700),
                Column(name="external_reference", type_name="text", type_oid=25),
                Column(name="idempotency_key", type_name="uuid", type_oid=2950),
                Column(name="recorded_at", type_name="timestamptz", type_oid=1184),
            ),
            rows=(
                {
                    "refund_id": "1",
                    "order_id": "order-88213",
                    "amount_usd": "250.00",
                    "external_reference": "SUP-88213",
                    "idempotency_key": "00000000-0000-4000-8000-000000000001",
                    "recorded_at": "2026-10-26T12:00:00Z",
                },
            ),
            truncated=True,
        ),
        Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "query", "The query exceeded maxRows."),
    )


setattr(query_truncated_lookup, "__module__", "dex_connectors.postgresql.postgresqlmock")


def query_missing_ledger_table() -> Case[RowSet]:
    return Case.branch_result(
        PROVIDER_REJECTED,
        RowSet(columns=(), rows=()),
        Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "query", "PostgreSQL could not find refund_ledger."),
    )


setattr(query_missing_ledger_table, "__module__", "dex_connectors.postgresql.postgresqlmock")


def execute_completed(value: StatementExecution, failure: Failure | None = None) -> Case[StatementExecution]:
    return Case.branch_result(COMPLETED, value, failure)


setattr(execute_completed, "__module__", "dex_connectors.postgresql.postgresqlmock")


def execute_limit_exceeded(
    value: StatementExecution, failure: Failure | None = None
) -> Case[StatementExecution]:
    return Case.branch_result(LIMIT_EXCEEDED, value, failure)


setattr(execute_limit_exceeded, "__module__", "dex_connectors.postgresql.postgresqlmock")


def execute_provider_rejected(
    value: StatementExecution, failure: Failure | None = None
) -> Case[StatementExecution]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


setattr(execute_provider_rejected, "__module__", "dex_connectors.postgresql.postgresqlmock")


def execute_uncertain(value: StatementExecution, failure: Failure) -> Case[StatementExecution]:
    return Case.uncertain(value, failure)


setattr(execute_uncertain, "__module__", "dex_connectors.postgresql.postgresqlmock")


def execute_invalid_response(
    value: StatementExecution, failure: Failure | None = None
) -> Case[StatementExecution]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)


setattr(execute_invalid_response, "__module__", "dex_connectors.postgresql.postgresqlmock")


def execute_defect(value: StatementExecution, failure: Failure | None = None) -> Case[StatementExecution]:
    return Case.branch_result(DEFECT, value, failure)


setattr(execute_defect, "__module__", "dex_connectors.postgresql.postgresqlmock")


def execute_retry(failure: Failure, after_seconds: float = 0) -> Case[StatementExecution]:
    return Case.retry(failure, after_seconds)


setattr(execute_retry, "__module__", "dex_connectors.postgresql.postgresqlmock")


def execute_recorded_refund() -> Case[StatementExecution]:
    return Case.branch_result(
        COMPLETED,
        StatementExecution(
            command="INSERT",
            rows_affected=1,
            columns=(
                Column(name="refund_id", type_name="int8", type_oid=20),
                Column(name="order_id", type_name="text", type_oid=25),
                Column(name="amount_usd", type_name="numeric", type_oid=1700),
                Column(name="external_reference", type_name="text", type_oid=25),
                Column(name="idempotency_key", type_name="uuid", type_oid=2950),
                Column(name="recorded_at", type_name="timestamptz", type_oid=1184),
            ),
            rows=(
                {
                    "refund_id": "1",
                    "order_id": "order-88213",
                    "amount_usd": "250.00",
                    "external_reference": "SUP-88213",
                    "idempotency_key": "00000000-0000-4000-8000-000000000001",
                    "recorded_at": "2026-10-26T12:00:00Z",
                },
            ),
        ),
    )


setattr(execute_recorded_refund, "__module__", "dex_connectors.postgresql.postgresqlmock")


def execute_affected_row_limit() -> Case[StatementExecution]:
    return Case.branch_result(
        LIMIT_EXCEEDED,
        StatementExecution(columns=(), rows=()),
        Failure(
            FailureKind.CONFLICT,
            CONNECTOR_ID,
            "execute",
            "The write exceeded maxRowsAffected and rolled back.",
        ),
    )


setattr(execute_affected_row_limit, "__module__", "dex_connectors.postgresql.postgresqlmock")


def execute_duplicate_order() -> Case[StatementExecution]:
    return Case.branch_result(
        PROVIDER_REJECTED,
        StatementExecution(columns=(), rows=()),
        Failure(FailureKind.CONFLICT, CONNECTOR_ID, "execute", "PostgreSQL rejected the duplicate order."),
    )


setattr(execute_duplicate_order, "__module__", "dex_connectors.postgresql.postgresqlmock")


class Mock:
    def __init__(self, connection_name: str = "postgresql") -> None:
        self.query_script: Query[QueryRowsInput, RowSet] = Query(QUERY_DEFINITION)
        self.query_script.default(query_no_recorded_refund())
        self.execute_script: Mutation[ExecuteStatementInput, StatementExecution] = Mutation(
            EXECUTE_DEFINITION
        )
        self.execute_script.default(execute_recorded_refund())
        self._connection = new_connection_with_operations(
            query=self.query_script,
            execute=self.execute_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection


setattr(Mock, "__module__", "dex_connectors.postgresql.postgresqlmock")

__all__ = [
    "Mock",
    "query_completed",
    "query_truncated",
    "query_provider_rejected",
    "query_invalid_response",
    "query_defect",
    "query_retry",
    "query_no_recorded_refund",
    "query_truncated_lookup",
    "query_missing_ledger_table",
    "execute_completed",
    "execute_limit_exceeded",
    "execute_provider_rejected",
    "execute_uncertain",
    "execute_invalid_response",
    "execute_defect",
    "execute_retry",
    "execute_recorded_refund",
    "execute_affected_row_limit",
    "execute_duplicate_order",
]
