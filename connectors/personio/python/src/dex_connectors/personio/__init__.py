# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Personio person Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetPersonOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetPersonInput, PersonProfile
from .steps import new_get_person_step

__all__ = [
    "PersonProfile",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetPersonInput",
    "GetPersonOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_person_step",
    "new_project_connection",
]
