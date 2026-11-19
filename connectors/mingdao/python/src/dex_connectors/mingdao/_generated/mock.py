# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListWorksheetsInput, ListWorksheetsOutput, Worksheet
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_WORKSHEETS_DEFINITION, PROVIDER_REJECTED

def list_worksheets_listed(value: ListWorksheetsOutput, failure: Failure | None = None) -> Case[ListWorksheetsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_worksheets_listed, '__module__', "dex_connectors.mingdao.mingdaomock")

def list_worksheets_provider_rejected(value: ListWorksheetsOutput, failure: Failure | None = None) -> Case[ListWorksheetsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_worksheets_provider_rejected, '__module__', "dex_connectors.mingdao.mingdaomock")

def list_worksheets_invalid_response(value: ListWorksheetsOutput, failure: Failure | None = None) -> Case[ListWorksheetsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_worksheets_invalid_response, '__module__', "dex_connectors.mingdao.mingdaomock")

def list_worksheets_defect(value: ListWorksheetsOutput, failure: Failure | None = None) -> Case[ListWorksheetsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_worksheets_defect, '__module__', "dex_connectors.mingdao.mingdaomock")

def list_worksheets_retry(failure: Failure, after_seconds: float = 0) -> Case[ListWorksheetsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_worksheets_retry, '__module__', "dex_connectors.mingdao.mingdaomock")

def list_worksheets_one_worksheet() -> Case[ListWorksheetsOutput]:
    return Case.branch_result(LISTED, ListWorksheetsOutput(worksheets=(Worksheet(id='sheet-123', name='客户表'),)))

setattr(list_worksheets_one_worksheet, '__module__', "dex_connectors.mingdao.mingdaomock")


class Mock:

    def __init__(self, connection_name: str = "mingdao") -> None:
        self.list_worksheets_script: Query[ListWorksheetsInput, ListWorksheetsOutput] = Query(LIST_WORKSHEETS_DEFINITION)
        self.list_worksheets_script.default(list_worksheets_one_worksheet())
        self._connection = new_connection_with_operations(
            list_worksheets=self.list_worksheets_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.mingdao.mingdaomock")

__all__ = [
    "Mock",
    "list_worksheets_listed",
    "list_worksheets_provider_rejected",
    "list_worksheets_invalid_response",
    "list_worksheets_defect",
    "list_worksheets_retry",
    "list_worksheets_one_worksheet",
]
