# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Supabase table row selection and upsert operations for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    SELECT_ROWS_DEFINITION,
    SELECTED,
    UPSERT_PREFERENCE,
    UPSERT_ROWS_DEFINITION,
    UPSERTED,
    Client,
    Config,
    Credentials,
    SelectRowsOperation,
    UpsertRowsOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import SelectRowsInput, SelectRowsOutput, UpsertRowsInput, UpsertRowsOutput
from .steps import new_select_rows_step, new_upsert_rows_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "SelectRowsInput",
    "SelectRowsOperation",
    "SelectRowsOutput",
    "UpsertRowsInput",
    "UpsertRowsOperation",
    "UpsertRowsOutput",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "new_select_rows_step",
    "new_upsert_rows_step",
    "CONNECTOR_ID",
    "DEFECT",
    "INVALID_RESPONSE",
    "PROVIDER_REJECTED",
    "SELECT_ROWS_DEFINITION",
    "SELECTED",
    "UPSERT_PREFERENCE",
    "UPSERT_ROWS_DEFINITION",
    "UPSERTED",
]
