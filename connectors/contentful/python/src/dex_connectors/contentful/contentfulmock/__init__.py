# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Contentful scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, GET_ENTRY_DEFINITION, LIST_ENTRIES_DEFINITION
from ..connection import Connection, new_connection_with_operations
from ..models import GetEntryInput, GetEntryOutput, ListEntriesInput, ListEntriesOutput


def get_entry_default_get_entry() -> Case[GetEntryOutput]:
    """Return the manifest's getEntry defaultGetEntry answer."""
    return Case.branch_result(
        "found",
        GetEntryOutput({"sys": {"id": "entry-meridian", "type": "Entry"}, "fields": {"title": "Meridian"}}),
    )


def list_entries_default_list_entries() -> Case[ListEntriesOutput]:
    """Return the manifest's listEntries defaultListEntries answer."""
    return Case.branch_result(
        "listed",
        ListEntriesOutput(
            {
                "items": [
                    {"sys": {"id": "entry-meridian", "type": "Entry"}, "fields": {"title": "Meridian"}}
                ],
                "total": 1,
                "skip": 0,
                "limit": 100,
            }
        ),
    )


class Mock:
    """Script Contentful Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "contentful") -> None:
        self.get_entry_script: Query[GetEntryInput, GetEntryOutput] = Query(GET_ENTRY_DEFINITION)
        self.get_entry_script.default(get_entry_default_get_entry())
        self.list_entries_script: Query[ListEntriesInput, ListEntriesOutput] = Query(LIST_ENTRIES_DEFINITION)
        self.list_entries_script.default(list_entries_default_list_entries())
        self._connection = new_connection_with_operations(
            self.get_entry_script, self.list_entries_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = ["Mock", "get_entry_default_get_entry", "list_entries_default_list_entries"]
