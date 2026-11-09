# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Microsoft Dynamics 365 Sales key Queries for Dex Flows."""

from .client import Client, Config, Credentials, FindContactOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import Contact, FindContactInput
from .steps import new_find_contact_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "FindContactInput",
    "Contact",
    "FindContactOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_find_contact_step",
    "new_project_connection",
]
