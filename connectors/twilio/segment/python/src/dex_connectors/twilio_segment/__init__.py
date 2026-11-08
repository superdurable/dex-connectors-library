# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Segment source Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetSourceOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetSourceInput, SourceProfile
from .steps import new_get_source_step

__all__ = [
    "SourceProfile",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetSourceInput",
    "GetSourceOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_source_step",
    "new_project_connection",
]
