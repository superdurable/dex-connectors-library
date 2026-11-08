# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Nimble Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetContactOperation, ListContactIDsOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import ContactIDsPage, ContactMetadata, GetContactInput, ListContactIDsInput
from .steps import new_get_contact_step, new_list_contact_ids_step

__all__ = [
    "Client", "Config", "Connection", "Credentials", "ContactMetadata", "ContactIDsPage",
    "GetContactInput", "GetContactOperation", "ListContactIDsInput", "ListContactIDsOperation",
    "new_connection", "new_connection_with_operations",
    "new_get_contact_step", "new_list_contact_ids_step", "new_project_connection",
]
