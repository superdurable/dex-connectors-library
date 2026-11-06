# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Google Sheets range and row operations."""

from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .connection import Connection as Connection
from .connection import new_connection as new_connection
from .connection import new_connection_with_operations as new_connection_with_operations
from .connection import new_project_connection as new_project_connection
from .models import FindRowInput as FindRowInput
from .models import FindRowOutput as FindRowOutput
from .models import GetValuesInput as GetValuesInput
from .models import GetValuesOutput as GetValuesOutput
from .models import UpsertRowInput as UpsertRowInput
from .models import UpsertRowOutput as UpsertRowOutput
from .operations import FindRowOperation as FindRowOperation
from .operations import GetValuesOperation as GetValuesOperation
from .operations import UpsertRowOperation as UpsertRowOperation
from .steps import new_find_row_step as new_find_row_step
from .steps import new_get_values_step as new_get_values_step
from .steps import new_upsert_row_step as new_upsert_row_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "FindRowInput",
    "FindRowOperation",
    "FindRowOutput",
    "GetValuesInput",
    "GetValuesOperation",
    "GetValuesOutput",
    "UpsertRowInput",
    "UpsertRowOperation",
    "UpsertRowOutput",
    "new_connection",
    "new_connection_with_operations",
    "new_find_row_step",
    "new_get_values_step",
    "new_project_connection",
    "new_upsert_row_step",
]
