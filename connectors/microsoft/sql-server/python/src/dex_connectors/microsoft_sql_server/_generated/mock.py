# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Column, ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution
from .definitions import CONNECTOR_ID, COMPLETED, DEFECT, EXECUTE_DEFINITION, INVALID_RESPONSE, LIMIT_EXCEEDED, PROVIDER_REJECTED, QUERY_DEFINITION, TRUNCATED, UNCERTAIN

def query_completed(value: RowSet, failure: Failure | None = None) -> Case[RowSet]:
    return Case.branch_result(COMPLETED, value, failure)

setattr(query_completed, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def query_truncated(value: RowSet, failure: Failure | None = None) -> Case[RowSet]:
    return Case.branch_result(TRUNCATED, value, failure)

setattr(query_truncated, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def query_provider_rejected(value: RowSet, failure: Failure | None = None) -> Case[RowSet]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_provider_rejected, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def query_invalid_response(value: RowSet, failure: Failure | None = None) -> Case[RowSet]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_invalid_response, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def query_defect(value: RowSet, failure: Failure | None = None) -> Case[RowSet]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_defect, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def query_retry(failure: Failure, after_seconds: float = 0) -> Case[RowSet]:
    return Case.retry(failure, after_seconds)

setattr(query_retry, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def query_completed_default_answer() -> Case[RowSet]:
    return Case.branch_result(COMPLETED, RowSet(columns=(Column(name='refund_id', type_name='BIGINT'), Column(name='order_id', type_name='NVARCHAR'), Column(name='amount_usd', type_name='DECIMAL'), Column(name='external_reference', type_name='NVARCHAR'), Column(name='idempotency_key', type_name='UNIQUEIDENTIFIER'), Column(name='recorded_at', type_name='DATETIME2')), rows=({'refund_id': '1', 'order_id': '88213', 'amount_usd': '250.00', 'external_reference': 'tkt_5488', 'idempotency_key': '6f1c2a9e-0000-4000-8000-000000000001', 'recorded_at': '2026-10-01T12:00:00.25'},)))

setattr(query_completed_default_answer, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def query_row_limit() -> Case[RowSet]:
    return Case.branch_result(TRUNCATED, RowSet(columns=(Column(name='refund_id', type_name='BIGINT'), Column(name='order_id', type_name='NVARCHAR'), Column(name='amount_usd', type_name='DECIMAL'), Column(name='external_reference', type_name='NVARCHAR'), Column(name='idempotency_key', type_name='UNIQUEIDENTIFIER'), Column(name='recorded_at', type_name='DATETIME2')), rows=({'refund_id': '1', 'order_id': '88213', 'amount_usd': '250.00', 'external_reference': 'tkt_5488', 'idempotency_key': '6f1c2a9e-0000-4000-8000-000000000001', 'recorded_at': '2026-10-01T12:00:00.25'},), truncated=True), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "query", "Querying refund_ledger for order 88213 with maxRows 1 below the result size returns a truncated row set."))

setattr(query_row_limit, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def query_missing_table() -> Case[RowSet]:
    return Case.branch_result(PROVIDER_REJECTED, RowSet(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "query", "Querying a missing refund_ledger table is rejected."))

setattr(query_missing_table, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def execute_completed(value: StatementExecution, failure: Failure | None = None) -> Case[StatementExecution]:
    return Case.branch_result(COMPLETED, value, failure)

setattr(execute_completed, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def execute_limit_exceeded(value: StatementExecution, failure: Failure | None = None) -> Case[StatementExecution]:
    return Case.branch_result(LIMIT_EXCEEDED, value, failure)

setattr(execute_limit_exceeded, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def execute_provider_rejected(value: StatementExecution, failure: Failure | None = None) -> Case[StatementExecution]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(execute_provider_rejected, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def execute_uncertain(value: StatementExecution, failure: Failure) -> Case[StatementExecution]:
    return Case.uncertain(value, failure)

setattr(execute_uncertain, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def execute_invalid_response(value: StatementExecution, failure: Failure | None = None) -> Case[StatementExecution]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(execute_invalid_response, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def execute_defect(value: StatementExecution, failure: Failure | None = None) -> Case[StatementExecution]:
    return Case.branch_result(DEFECT, value, failure)

setattr(execute_defect, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def execute_retry(failure: Failure, after_seconds: float = 0) -> Case[StatementExecution]:
    return Case.retry(failure, after_seconds)

setattr(execute_retry, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def execute_completed_default_answer() -> Case[StatementExecution]:
    return Case.branch_result(COMPLETED, StatementExecution(rows_affected=1, columns=(Column(name='refund_id', type_name='BIGINT'), Column(name='order_id', type_name='NVARCHAR'), Column(name='amount_usd', type_name='DECIMAL'), Column(name='external_reference', type_name='NVARCHAR'), Column(name='idempotency_key', type_name='UNIQUEIDENTIFIER'), Column(name='recorded_at', type_name='DATETIME2')), rows=({'refund_id': '1', 'order_id': '88213', 'amount_usd': '250.00', 'external_reference': 'tkt_5488', 'idempotency_key': '6f1c2a9e-0000-4000-8000-000000000001', 'recorded_at': '2026-10-01T12:00:00.25'},)))

setattr(execute_completed_default_answer, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def execute_race_lost() -> Case[StatementExecution]:
    return Case.branch_result(COMPLETED, StatementExecution(rows_affected=0, columns=(Column(name='refund_id', type_name='BIGINT'), Column(name='order_id', type_name='NVARCHAR'), Column(name='amount_usd', type_name='DECIMAL'), Column(name='external_reference', type_name='NVARCHAR'), Column(name='idempotency_key', type_name='UNIQUEIDENTIFIER'), Column(name='recorded_at', type_name='DATETIME2')), rows=()))

setattr(execute_race_lost, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def execute_row_limit() -> Case[StatementExecution]:
    return Case.branch_result(LIMIT_EXCEEDED, StatementExecution(), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "execute", "Updating more rows than maxRowsAffected rolls back the statement."))

setattr(execute_row_limit, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

def execute_duplicate_key() -> Case[StatementExecution]:
    return Case.branch_result(PROVIDER_REJECTED, StatementExecution(), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "execute", "Inserting a duplicate unique key is rejected before commit."))

setattr(execute_duplicate_key, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")


class Mock:

    def __init__(self, connection_name: str = "microsoft-sql-server") -> None:
        self.query_script: Query[QueryRowsInput, RowSet] = Query(QUERY_DEFINITION)
        self.query_script.default(query_completed_default_answer())
        self.execute_script: Mutation[ExecuteStatementInput, StatementExecution] = Mutation(EXECUTE_DEFINITION)
        self.execute_script.default(execute_completed_default_answer())
        self._connection = new_connection_with_operations(
            query=self.query_script,
            execute=self.execute_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.microsoft_sql_server.sqlservermock")

__all__ = [
    "Mock",
    "query_completed",
    "query_truncated",
    "query_provider_rejected",
    "query_invalid_response",
    "query_defect",
    "query_retry",
    "query_completed_default_answer",
    "query_row_limit",
    "query_missing_table",
    "execute_completed",
    "execute_limit_exceeded",
    "execute_provider_rejected",
    "execute_uncertain",
    "execute_invalid_response",
    "execute_defect",
    "execute_retry",
    "execute_completed_default_answer",
    "execute_race_lost",
    "execute_row_limit",
    "execute_duplicate_key",
]
