# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CancelStatementInput, Column, GetStatementResultInput, StatementCancellation, StatementResult, StatementStats, StatementSubmission, SubmitStatementInput
from .definitions import CONNECTOR_ID, CANCELED, CANCEL_STATEMENT_DEFINITION, COMPLETED, DEFECT, GET_STATEMENT_RESULT_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED, RUNNING, SUBMITTED, SUBMIT_STATEMENT_DEFINITION, TRUNCATED

def submit_statement_submitted(value: StatementSubmission, failure: Failure | None = None) -> Case[StatementSubmission]:
    return Case.branch_result(SUBMITTED, value, failure)

setattr(submit_statement_submitted, '__module__', "dex_connectors.snowflake.snowflakemock")

def submit_statement_provider_rejected(value: StatementSubmission, failure: Failure | None = None) -> Case[StatementSubmission]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(submit_statement_provider_rejected, '__module__', "dex_connectors.snowflake.snowflakemock")

def submit_statement_defect(value: StatementSubmission, failure: Failure | None = None) -> Case[StatementSubmission]:
    return Case.branch_result(DEFECT, value, failure)

setattr(submit_statement_defect, '__module__', "dex_connectors.snowflake.snowflakemock")

def submit_statement_retry(failure: Failure, after_seconds: float = 0) -> Case[StatementSubmission]:
    return Case.retry(failure, after_seconds)

setattr(submit_statement_retry, '__module__', "dex_connectors.snowflake.snowflakemock")

def submit_statement_accepted_account_usage_query() -> Case[StatementSubmission]:
    return Case.branch_result(SUBMITTED, StatementSubmission(statement_handle='00000000-0000-4000-8000-000000000003', submitted_at=datetime.fromisoformat("2026-10-26T09:00:00Z".replace('Z', '+00:00'))))

setattr(submit_statement_accepted_account_usage_query, '__module__', "dex_connectors.snowflake.snowflakemock")

def submit_statement_invalid_sql() -> Case[StatementSubmission]:
    return Case.branch_result(PROVIDER_REJECTED, StatementSubmission(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "submitStatement", "Snowflake rejected the statement at compilation."))

setattr(submit_statement_invalid_sql, '__module__', "dex_connectors.snowflake.snowflakemock")

def get_statement_result_completed(value: StatementResult, failure: Failure | None = None) -> Case[StatementResult]:
    return Case.branch_result(COMPLETED, value, failure)

setattr(get_statement_result_completed, '__module__', "dex_connectors.snowflake.snowflakemock")

def get_statement_result_running(value: StatementResult, failure: Failure | None = None) -> Case[StatementResult]:
    return Case.branch_result(RUNNING, value, failure)

setattr(get_statement_result_running, '__module__', "dex_connectors.snowflake.snowflakemock")

def get_statement_result_truncated(value: StatementResult, failure: Failure | None = None) -> Case[StatementResult]:
    return Case.branch_result(TRUNCATED, value, failure)

setattr(get_statement_result_truncated, '__module__', "dex_connectors.snowflake.snowflakemock")

def get_statement_result_provider_rejected(value: StatementResult, failure: Failure | None = None) -> Case[StatementResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_statement_result_provider_rejected, '__module__', "dex_connectors.snowflake.snowflakemock")

def get_statement_result_invalid_response(value: StatementResult, failure: Failure | None = None) -> Case[StatementResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_statement_result_invalid_response, '__module__', "dex_connectors.snowflake.snowflakemock")

def get_statement_result_defect(value: StatementResult, failure: Failure | None = None) -> Case[StatementResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_statement_result_defect, '__module__', "dex_connectors.snowflake.snowflakemock")

def get_statement_result_retry(failure: Failure, after_seconds: float = 0) -> Case[StatementResult]:
    return Case.retry(failure, after_seconds)

setattr(get_statement_result_retry, '__module__', "dex_connectors.snowflake.snowflakemock")

def get_statement_result_completed_account_usage_query() -> Case[StatementResult]:
    return Case.branch_result(COMPLETED, StatementResult(statement_handle='00000000-0000-4000-8000-000000000003', columns=(Column(name='EVENT_COUNT', type='fixed', precision=18), Column(name='CREDITS_USED', type='fixed', precision=38, scale=4), Column(name='LAST_EVENT_AT', type='timestamp_ntz', scale=9, nullable=True)), rows=({'EVENT_COUNT': '128', 'CREDITS_USED': '42.5000', 'LAST_EVENT_AT': '2026-01-01T00:00:00.5'},), partition=0, partition_count=1, total_row_count=1))

setattr(get_statement_result_completed_account_usage_query, '__module__', "dex_connectors.snowflake.snowflakemock")

def get_statement_result_query_still_running() -> Case[StatementResult]:
    return Case.branch_result(RUNNING, StatementResult(statement_handle='00000000-0000-4000-8000-000000000003', columns=(), rows=(), partition=0))

setattr(get_statement_result_query_still_running, '__module__', "dex_connectors.snowflake.snowflakemock")

def get_statement_result_oversized_partition() -> Case[StatementResult]:
    return Case.branch_result(TRUNCATED, StatementResult(statement_handle='00000000-0000-4000-8000-000000000003', columns=(Column(name='EVENT_COUNT', type='fixed', precision=18),), rows=({'EVENT_COUNT': '128'},), partition=0, partition_count=1, total_row_count=2, truncated=True), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "getStatementResult", "The result partition exceeded maxRows."))

setattr(get_statement_result_oversized_partition, '__module__', "dex_connectors.snowflake.snowflakemock")

def get_statement_result_failed_statement() -> Case[StatementResult]:
    return Case.branch_result(PROVIDER_REJECTED, StatementResult(statement_handle='00000000-0000-4000-8000-000000000003', columns=(), rows=(), partition=0), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "getStatementResult", "Snowflake could not convert the statement value."))

setattr(get_statement_result_failed_statement, '__module__', "dex_connectors.snowflake.snowflakemock")

def cancel_statement_canceled(value: StatementCancellation, failure: Failure | None = None) -> Case[StatementCancellation]:
    return Case.branch_result(CANCELED, value, failure)

setattr(cancel_statement_canceled, '__module__', "dex_connectors.snowflake.snowflakemock")

def cancel_statement_provider_rejected(value: StatementCancellation, failure: Failure | None = None) -> Case[StatementCancellation]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(cancel_statement_provider_rejected, '__module__', "dex_connectors.snowflake.snowflakemock")

def cancel_statement_defect(value: StatementCancellation, failure: Failure | None = None) -> Case[StatementCancellation]:
    return Case.branch_result(DEFECT, value, failure)

setattr(cancel_statement_defect, '__module__', "dex_connectors.snowflake.snowflakemock")

def cancel_statement_retry(failure: Failure, after_seconds: float = 0) -> Case[StatementCancellation]:
    return Case.retry(failure, after_seconds)

setattr(cancel_statement_retry, '__module__', "dex_connectors.snowflake.snowflakemock")

def cancel_statement_canceled_account_usage_query() -> Case[StatementCancellation]:
    return Case.branch_result(CANCELED, StatementCancellation(statement_handle='00000000-0000-4000-8000-000000000003'))

setattr(cancel_statement_canceled_account_usage_query, '__module__', "dex_connectors.snowflake.snowflakemock")

def cancel_statement_unknown_statement_handle() -> Case[StatementCancellation]:
    return Case.branch_result(PROVIDER_REJECTED, StatementCancellation(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "cancelStatement", "Snowflake could not find the statement."))

setattr(cancel_statement_unknown_statement_handle, '__module__', "dex_connectors.snowflake.snowflakemock")


class Mock:

    def __init__(self, connection_name: str = "snowflake") -> None:
        self.submit_statement_script: Mutation[SubmitStatementInput, StatementSubmission] = Mutation(SUBMIT_STATEMENT_DEFINITION)
        self.submit_statement_script.default(submit_statement_accepted_account_usage_query())
        self.get_statement_result_script: Query[GetStatementResultInput, StatementResult] = Query(GET_STATEMENT_RESULT_DEFINITION)
        self.get_statement_result_script.default(get_statement_result_completed_account_usage_query())
        self.cancel_statement_script: Mutation[CancelStatementInput, StatementCancellation] = Mutation(CANCEL_STATEMENT_DEFINITION)
        self.cancel_statement_script.default(cancel_statement_canceled_account_usage_query())
        self._connection = new_connection_with_operations(
            submit_statement=self.submit_statement_script,
            get_statement_result=self.get_statement_result_script,
            cancel_statement=self.cancel_statement_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.snowflake.snowflakemock")

__all__ = [
    "Mock",
    "submit_statement_submitted",
    "submit_statement_provider_rejected",
    "submit_statement_defect",
    "submit_statement_retry",
    "submit_statement_accepted_account_usage_query",
    "submit_statement_invalid_sql",
    "get_statement_result_completed",
    "get_statement_result_running",
    "get_statement_result_truncated",
    "get_statement_result_provider_rejected",
    "get_statement_result_invalid_response",
    "get_statement_result_defect",
    "get_statement_result_retry",
    "get_statement_result_completed_account_usage_query",
    "get_statement_result_query_still_running",
    "get_statement_result_oversized_partition",
    "get_statement_result_failed_statement",
    "cancel_statement_canceled",
    "cancel_statement_provider_rejected",
    "cancel_statement_defect",
    "cancel_statement_retry",
    "cancel_statement_canceled_account_usage_query",
    "cancel_statement_unknown_statement_handle",
]
