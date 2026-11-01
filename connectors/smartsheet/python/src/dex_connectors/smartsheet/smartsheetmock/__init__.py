# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Smartsheet scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_SHEET_DEFINITION,
    LIST_SHEETS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import GetSheetInput, ListSheetsInput, Sheet, SheetColumn, SheetPage, SheetSummary


def list_sheets_listed(value: SheetPage, failure: Failure | None = None) -> Case[SheetPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_sheets_default_answer() -> Case[SheetPage]:
    """Return the manifest's listSheets defaultAnswer."""
    return list_sheets_listed(SheetPage((SheetSummary(1001, "Meridian Planning"),), 1, 1))


def list_sheets_invalid_key() -> Case[SheetPage]:
    """Return the manifest's listSheets invalidKey answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        SheetPage(),
        Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listSheets", "Smartsheet rejected the token."),
    )


def get_sheet_found(value: Sheet, failure: Failure | None = None) -> Case[Sheet]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_sheet_not_found(value: Sheet, failure: Failure | None = None) -> Case[Sheet]:
    """Script a missing sheet."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_sheet_default_answer() -> Case[Sheet]:
    """Return the manifest's getSheet defaultAnswer."""
    return get_sheet_found(Sheet(1001, "Meridian Planning", (SheetColumn(2001, "Task"),), ()))


def get_sheet_invalid_key() -> Case[Sheet]:
    """Return the manifest's getSheet invalidKey answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        Sheet(),
        Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getSheet", "Smartsheet rejected the token."),
    )


class Mock:
    """Script Smartsheet Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "smartsheet") -> None:
        self.list_sheets_script: Query[ListSheetsInput, SheetPage] = Query(LIST_SHEETS_DEFINITION)
        self.list_sheets_script.default(list_sheets_default_answer())
        self.get_sheet_script: Query[GetSheetInput, Sheet] = Query(GET_SHEET_DEFINITION)
        self.get_sheet_script.default(get_sheet_default_answer())
        self._connection = new_connection_with_operations(
            self.list_sheets_script, self.get_sheet_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_sheets_listed",
    "list_sheets_default_answer",
    "list_sheets_invalid_key",
    "get_sheet_found",
    "get_sheet_not_found",
    "get_sheet_default_answer",
    "get_sheet_invalid_key",
]
