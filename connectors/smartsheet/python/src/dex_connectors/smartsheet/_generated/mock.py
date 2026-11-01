# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetSheetInput, ListSheetsInput, Sheet, SheetColumn, SheetPage, SheetRow, SheetSummary
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SHEET_DEFINITION, INVALID_RESPONSE, LISTED, LIST_SHEETS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_sheets_listed(value: SheetPage, failure: Failure | None = None) -> Case[SheetPage]:
    """Script the listed branch."""
    return Case.branch_result(LISTED, value, failure)

setattr(list_sheets_listed, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def list_sheets_provider_rejected(value: SheetPage, failure: Failure | None = None) -> Case[SheetPage]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_sheets_provider_rejected, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def list_sheets_invalid_response(value: SheetPage, failure: Failure | None = None) -> Case[SheetPage]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_sheets_invalid_response, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def list_sheets_defect(value: SheetPage, failure: Failure | None = None) -> Case[SheetPage]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(list_sheets_defect, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def list_sheets_retry(failure: Failure, after_seconds: float = 0) -> Case[SheetPage]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(list_sheets_retry, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def list_sheets_default_answer() -> Case[SheetPage]:
    """Return the manifest defaultAnswer outcome."""
    return Case.branch_result(LISTED, SheetPage(sheets=(SheetSummary(id=1001, name='Meridian Planning'),), page_number=1, total_count=1))

setattr(list_sheets_default_answer, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def list_sheets_invalid_key() -> Case[SheetPage]:
    """Return the manifest invalidKey outcome."""
    return Case.branch_result(PROVIDER_REJECTED, SheetPage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listSheets", "Smartsheet rejected the token."))

setattr(list_sheets_invalid_key, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def get_sheet_found(value: Sheet, failure: Failure | None = None) -> Case[Sheet]:
    """Script the found branch."""
    return Case.branch_result(FOUND, value, failure)

setattr(get_sheet_found, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def get_sheet_not_found(value: Sheet, failure: Failure | None = None) -> Case[Sheet]:
    """Script the notFound branch."""
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_sheet_not_found, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def get_sheet_provider_rejected(value: Sheet, failure: Failure | None = None) -> Case[Sheet]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_sheet_provider_rejected, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def get_sheet_invalid_response(value: Sheet, failure: Failure | None = None) -> Case[Sheet]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_sheet_invalid_response, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def get_sheet_defect(value: Sheet, failure: Failure | None = None) -> Case[Sheet]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(get_sheet_defect, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def get_sheet_retry(failure: Failure, after_seconds: float = 0) -> Case[Sheet]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(get_sheet_retry, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def get_sheet_default_answer() -> Case[Sheet]:
    """Return the manifest defaultAnswer outcome."""
    return Case.branch_result(FOUND, Sheet(id=1001, name='Meridian Planning', columns=(SheetColumn(id=2001, title='Task'),), rows=()))

setattr(get_sheet_default_answer, '__module__', "dex_connectors.smartsheet.smartsheetmock")

def get_sheet_invalid_key() -> Case[Sheet]:
    """Return the manifest invalidKey outcome."""
    return Case.branch_result(PROVIDER_REJECTED, Sheet(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getSheet", "Smartsheet rejected the token."))

setattr(get_sheet_invalid_key, '__module__', "dex_connectors.smartsheet.smartsheetmock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "smartsheet") -> None:
        self.list_sheets_script: Query[ListSheetsInput, SheetPage] = Query(LIST_SHEETS_DEFINITION)
        self.list_sheets_script.default(list_sheets_default_answer())
        self.get_sheet_script: Query[GetSheetInput, Sheet] = Query(GET_SHEET_DEFINITION)
        self.get_sheet_script.default(get_sheet_default_answer())
        self._connection = new_connection_with_operations(
            list_sheets=self.list_sheets_script,
            get_sheet=self.get_sheet_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.smartsheet.smartsheetmock")

__all__ = [
    "Mock",
    "list_sheets_listed",
    "list_sheets_provider_rejected",
    "list_sheets_invalid_response",
    "list_sheets_defect",
    "list_sheets_retry",
    "list_sheets_default_answer",
    "list_sheets_invalid_key",
    "get_sheet_found",
    "get_sheet_not_found",
    "get_sheet_provider_rejected",
    "get_sheet_invalid_response",
    "get_sheet_defect",
    "get_sheet_retry",
    "get_sheet_default_answer",
    "get_sheet_invalid_key",
]
