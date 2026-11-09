# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetStatementResultInput, StatementResultPage
from .definitions import CONNECTOR_ID, DEFECT, FETCHED, GET_STATEMENT_RESULT_DEFINITION, INVALID_RESPONSE, MISSING, PROVIDER_REJECTED

def get_statement_result_fetched(value: StatementResultPage, failure: Failure | None = None) -> Case[StatementResultPage]:
    return Case.branch_result(FETCHED, value, failure)

setattr(get_statement_result_fetched, '__module__', "dex_connectors.amazon_redshift.redshiftmock")

def get_statement_result_missing(value: StatementResultPage, failure: Failure | None = None) -> Case[StatementResultPage]:
    return Case.branch_result(MISSING, value, failure)

setattr(get_statement_result_missing, '__module__', "dex_connectors.amazon_redshift.redshiftmock")

def get_statement_result_provider_rejected(value: StatementResultPage, failure: Failure | None = None) -> Case[StatementResultPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_statement_result_provider_rejected, '__module__', "dex_connectors.amazon_redshift.redshiftmock")

def get_statement_result_invalid_response(value: StatementResultPage, failure: Failure | None = None) -> Case[StatementResultPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_statement_result_invalid_response, '__module__', "dex_connectors.amazon_redshift.redshiftmock")

def get_statement_result_defect(value: StatementResultPage, failure: Failure | None = None) -> Case[StatementResultPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_statement_result_defect, '__module__', "dex_connectors.amazon_redshift.redshiftmock")

def get_statement_result_retry(failure: Failure, after_seconds: float = 0) -> Case[StatementResultPage]:
    return Case.retry(failure, after_seconds)

setattr(get_statement_result_retry, '__module__', "dex_connectors.amazon_redshift.redshiftmock")

def get_statement_result_fetched_default() -> Case[StatementResultPage]:
    return Case.branch_result(FETCHED, StatementResultPage(records=([{'stringValue': 'example'}],), next_token='', total_num_rows=1))

setattr(get_statement_result_fetched_default, '__module__', "dex_connectors.amazon_redshift.redshiftmock")


class Mock:

    def __init__(self, connection_name: str = "amazon-redshift") -> None:
        self.get_statement_result_script: Query[GetStatementResultInput, StatementResultPage] = Query(GET_STATEMENT_RESULT_DEFINITION)
        self.get_statement_result_script.default(get_statement_result_fetched_default())
        self._connection = new_connection_with_operations(
            get_statement_result=self.get_statement_result_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.amazon_redshift.redshiftmock")

__all__ = [
    "Mock",
    "get_statement_result_fetched",
    "get_statement_result_missing",
    "get_statement_result_provider_rejected",
    "get_statement_result_invalid_response",
    "get_statement_result_defect",
    "get_statement_result_retry",
    "get_statement_result_fetched_default",
]
