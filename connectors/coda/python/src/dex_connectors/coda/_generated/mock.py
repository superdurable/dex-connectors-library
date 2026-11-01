# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetRowInput, GetRowOutput, ListRowsInput, ListRowsOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ROW_DEFINITION, INVALID_RESPONSE, LISTED, LIST_ROWS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_rows_listed(value: ListRowsOutput, failure: Failure | None = None) -> Case[ListRowsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_rows_listed, '__module__', "dex_connectors.coda.codamock")

def list_rows_provider_rejected(value: ListRowsOutput, failure: Failure | None = None) -> Case[ListRowsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_rows_provider_rejected, '__module__', "dex_connectors.coda.codamock")

def list_rows_invalid_response(value: ListRowsOutput, failure: Failure | None = None) -> Case[ListRowsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_rows_invalid_response, '__module__', "dex_connectors.coda.codamock")

def list_rows_defect(value: ListRowsOutput, failure: Failure | None = None) -> Case[ListRowsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_rows_defect, '__module__', "dex_connectors.coda.codamock")

def list_rows_retry(failure: Failure, after_seconds: float = 0) -> Case[ListRowsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_rows_retry, '__module__', "dex_connectors.coda.codamock")

def list_rows_default_list_rows() -> Case[ListRowsOutput]:
    return Case.branch_result(LISTED, ListRowsOutput(data={'items': [{'id': 'i-Meridian', 'name': 'Meridian', 'values': {'c-Name': 'Meridian'}}], 'nextPageToken': ''}))

setattr(list_rows_default_list_rows, '__module__', "dex_connectors.coda.codamock")

def get_row_found(value: GetRowOutput, failure: Failure | None = None) -> Case[GetRowOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_row_found, '__module__', "dex_connectors.coda.codamock")

def get_row_not_found(value: GetRowOutput, failure: Failure | None = None) -> Case[GetRowOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_row_not_found, '__module__', "dex_connectors.coda.codamock")

def get_row_provider_rejected(value: GetRowOutput, failure: Failure | None = None) -> Case[GetRowOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_row_provider_rejected, '__module__', "dex_connectors.coda.codamock")

def get_row_invalid_response(value: GetRowOutput, failure: Failure | None = None) -> Case[GetRowOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_row_invalid_response, '__module__', "dex_connectors.coda.codamock")

def get_row_defect(value: GetRowOutput, failure: Failure | None = None) -> Case[GetRowOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_row_defect, '__module__', "dex_connectors.coda.codamock")

def get_row_retry(failure: Failure, after_seconds: float = 0) -> Case[GetRowOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_row_retry, '__module__', "dex_connectors.coda.codamock")

def get_row_default_get_row() -> Case[GetRowOutput]:
    return Case.branch_result(FOUND, GetRowOutput(data={'id': 'i-Meridian', 'name': 'Meridian', 'values': {'c-Name': 'Meridian'}}))

setattr(get_row_default_get_row, '__module__', "dex_connectors.coda.codamock")


class Mock:

    def __init__(self, connection_name: str = "coda") -> None:
        self.list_rows_script: Query[ListRowsInput, ListRowsOutput] = Query(LIST_ROWS_DEFINITION)
        self.list_rows_script.default(list_rows_default_list_rows())
        self.get_row_script: Query[GetRowInput, GetRowOutput] = Query(GET_ROW_DEFINITION)
        self.get_row_script.default(get_row_default_get_row())
        self._connection = new_connection_with_operations(
            list_rows=self.list_rows_script,
            get_row=self.get_row_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.coda.codamock")

__all__ = [
    "Mock",
    "list_rows_listed",
    "list_rows_provider_rejected",
    "list_rows_invalid_response",
    "list_rows_defect",
    "list_rows_retry",
    "list_rows_default_list_rows",
    "get_row_found",
    "get_row_not_found",
    "get_row_provider_rejected",
    "get_row_invalid_response",
    "get_row_defect",
    "get_row_retry",
    "get_row_default_get_row",
]
