# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Coda scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, GET_ROW_DEFINITION, LIST_ROWS_DEFINITION
from ..connection import Connection, new_connection_with_operations
from ..models import GetRowInput, GetRowOutput, ListRowsInput, ListRowsOutput


def list_rows_default_list_rows() -> Case[ListRowsOutput]:
    """Return the manifest's listRows defaultListRows answer."""
    return Case.branch_result(
        "listed",
        ListRowsOutput(
            {
                "items": [{"id": "i-Meridian", "name": "Meridian", "values": {"c-Name": "Meridian"}}],
                "nextPageToken": "",
            }
        ),
    )


def get_row_default_get_row() -> Case[GetRowOutput]:
    """Return the manifest's getRow defaultGetRow answer."""
    return Case.branch_result(
        "found", GetRowOutput({"id": "i-Meridian", "name": "Meridian", "values": {"c-Name": "Meridian"}})
    )


class Mock:
    """Script Coda Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "coda") -> None:
        self.list_rows_script: Query[ListRowsInput, ListRowsOutput] = Query(LIST_ROWS_DEFINITION)
        self.list_rows_script.default(list_rows_default_list_rows())
        self.get_row_script: Query[GetRowInput, GetRowOutput] = Query(GET_ROW_DEFINITION)
        self.get_row_script.default(get_row_default_get_row())
        self._connection = new_connection_with_operations(
            self.list_rows_script, self.get_row_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = ["Mock", "list_rows_default_list_rows", "get_row_default_get_row"]
