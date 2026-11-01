# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Supabase scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..client import CONNECTOR_ID, SELECT_ROWS_DEFINITION, SELECTED, UPSERT_ROWS_DEFINITION, UPSERTED
from ..connection import Connection, new_connection_with_operations
from ..models import SelectRowsInput, SelectRowsOutput, UpsertRowsInput, UpsertRowsOutput


def select_rows_selected(value: SelectRowsOutput, failure: Failure | None = None) -> Case[SelectRowsOutput]:
    """Script the selected route."""
    return Case.branch_result(SELECTED, value, failure)


def select_rows_default_answer() -> Case[SelectRowsOutput]:
    """Return the manifest's selectRows default answer."""
    return select_rows_selected(SelectRowsOutput([{"id": 1, "name": "Meridian"}]))


def upsert_rows_upserted(value: UpsertRowsOutput, failure: Failure | None = None) -> Case[UpsertRowsOutput]:
    """Script the upserted route."""
    return Case.branch_result(UPSERTED, value, failure)


def upsert_rows_default_answer() -> Case[UpsertRowsOutput]:
    """Return the manifest's upsertRows default answer."""
    return upsert_rows_upserted(UpsertRowsOutput([{"id": 1, "name": "Meridian"}]))


class Mock:
    """Script Supabase operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "supabase") -> None:
        self.select_rows_script: Query[SelectRowsInput, SelectRowsOutput] = Query(SELECT_ROWS_DEFINITION)
        self.select_rows_script.default(select_rows_default_answer())
        self.upsert_rows_script: Mutation[UpsertRowsInput, UpsertRowsOutput] = Mutation(
            UPSERT_ROWS_DEFINITION
        )
        self.upsert_rows_script.default(upsert_rows_default_answer())
        self._connection = new_connection_with_operations(
            self.upsert_rows_script, self.select_rows_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "select_rows_selected",
    "select_rows_default_answer",
    "upsert_rows_upserted",
    "upsert_rows_default_answer",
]
