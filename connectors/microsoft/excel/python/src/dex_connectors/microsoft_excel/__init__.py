# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Microsoft Excel operations for Dex Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_project_connection
from .models import (
    AppendTableRowsInput,
    AppendTableRowsOutput,
    GetTableRowsInput,
    GetTableRowsOutput,
    GetValuesInput,
    GetValuesOutput,
    UpdateValuesInput,
    UpdateValuesOutput,
)
from .operations import (
    AppendTableRowsOperation,
    GetTableRowsOperation,
    GetValuesOperation,
    UpdateValuesOperation,
)
from .steps import (
    new_append_table_rows_step,
    new_get_table_rows_step,
    new_get_values_step,
    new_update_values_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_project_connection",
    "AppendTableRowsInput",
    "AppendTableRowsOutput",
    "GetTableRowsInput",
    "GetTableRowsOutput",
    "GetValuesInput",
    "GetValuesOutput",
    "UpdateValuesInput",
    "UpdateValuesOutput",
    "AppendTableRowsOperation",
    "GetTableRowsOperation",
    "GetValuesOperation",
    "UpdateValuesOperation",
    "new_append_table_rows_step",
    "new_get_table_rows_step",
    "new_get_values_step",
    "new_update_values_step",
]
