# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import SelectRowsInput, SelectRowsOutput, UpsertRowsInput, UpsertRowsOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED, SELECTED, SELECT_ROWS_DEFINITION, UPSERTED, UPSERT_ROWS_DEFINITION

def select_rows_selected(value: SelectRowsOutput, failure: Failure | None = None) -> Case[SelectRowsOutput]:
    """Script the selected branch."""
    return Case.branch_result(SELECTED, value, failure)

setattr(select_rows_selected, '__module__', "dex_connectors.supabase.supabasemock")

def select_rows_provider_rejected(value: SelectRowsOutput, failure: Failure | None = None) -> Case[SelectRowsOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(select_rows_provider_rejected, '__module__', "dex_connectors.supabase.supabasemock")

def select_rows_invalid_response(value: SelectRowsOutput, failure: Failure | None = None) -> Case[SelectRowsOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(select_rows_invalid_response, '__module__', "dex_connectors.supabase.supabasemock")

def select_rows_defect(value: SelectRowsOutput, failure: Failure | None = None) -> Case[SelectRowsOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(select_rows_defect, '__module__', "dex_connectors.supabase.supabasemock")

def select_rows_retry(failure: Failure, after_seconds: float = 0) -> Case[SelectRowsOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(select_rows_retry, '__module__', "dex_connectors.supabase.supabasemock")

def select_rows_default_select_rows() -> Case[SelectRowsOutput]:
    """Return the manifest defaultSelectRows outcome."""
    return Case.branch_result(SELECTED, SelectRowsOutput(data=[{'id': 1, 'name': 'Meridian'}]))

setattr(select_rows_default_select_rows, '__module__', "dex_connectors.supabase.supabasemock")

def upsert_rows_upserted(value: UpsertRowsOutput, failure: Failure | None = None) -> Case[UpsertRowsOutput]:
    """Script the upserted branch."""
    return Case.branch_result(UPSERTED, value, failure)

setattr(upsert_rows_upserted, '__module__', "dex_connectors.supabase.supabasemock")

def upsert_rows_provider_rejected(value: UpsertRowsOutput, failure: Failure | None = None) -> Case[UpsertRowsOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upsert_rows_provider_rejected, '__module__', "dex_connectors.supabase.supabasemock")

def upsert_rows_invalid_response(value: UpsertRowsOutput, failure: Failure | None = None) -> Case[UpsertRowsOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upsert_rows_invalid_response, '__module__', "dex_connectors.supabase.supabasemock")

def upsert_rows_defect(value: UpsertRowsOutput, failure: Failure | None = None) -> Case[UpsertRowsOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(upsert_rows_defect, '__module__', "dex_connectors.supabase.supabasemock")

def upsert_rows_retry(failure: Failure, after_seconds: float = 0) -> Case[UpsertRowsOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(upsert_rows_retry, '__module__', "dex_connectors.supabase.supabasemock")

def upsert_rows_default_upsert_rows() -> Case[UpsertRowsOutput]:
    """Return the manifest defaultUpsertRows outcome."""
    return Case.branch_result(UPSERTED, UpsertRowsOutput(data=[{'id': 1, 'name': 'Meridian'}]))

setattr(upsert_rows_default_upsert_rows, '__module__', "dex_connectors.supabase.supabasemock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "supabase") -> None:
        self.select_rows_script: Query[SelectRowsInput, SelectRowsOutput] = Query(SELECT_ROWS_DEFINITION)
        self.select_rows_script.default(select_rows_default_select_rows())
        self.upsert_rows_script: Mutation[UpsertRowsInput, UpsertRowsOutput] = Mutation(UPSERT_ROWS_DEFINITION)
        self.upsert_rows_script.default(upsert_rows_default_upsert_rows())
        self._connection = new_connection_with_operations(
            select_rows=self.select_rows_script,
            upsert_rows=self.upsert_rows_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.supabase.supabasemock")

__all__ = [
    "Mock",
    "select_rows_selected",
    "select_rows_provider_rejected",
    "select_rows_invalid_response",
    "select_rows_defect",
    "select_rows_retry",
    "select_rows_default_select_rows",
    "upsert_rows_upserted",
    "upsert_rows_provider_rejected",
    "upsert_rows_invalid_response",
    "upsert_rows_defect",
    "upsert_rows_retry",
    "upsert_rows_default_upsert_rows",
]
