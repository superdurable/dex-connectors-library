# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Creatio contact Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetContactOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import ContactSummary, GetContactInput
from .steps import new_get_contact_step

__all__ = [
    "ContactSummary",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetContactInput",
    "GetContactOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_contact_step",
    "new_project_connection",
]
