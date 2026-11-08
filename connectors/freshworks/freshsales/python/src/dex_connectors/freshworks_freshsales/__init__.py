# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Freshsales Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetContactOperation, ListContactViewsOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import ContactMetadata, ContactViewMetadata, ContactViews, GetContactInput, ListContactViewsInput
from .steps import new_get_contact_step, new_list_contact_views_step

__all__ = [
    "Client", "Config", "Connection", "Credentials", "ContactMetadata", "ContactViews", "ContactViewMetadata",
    "GetContactInput", "GetContactOperation", "ListContactViewsInput", "ListContactViewsOperation",
    "new_connection", "new_connection_with_operations",
    "new_get_contact_step", "new_list_contact_views_step", "new_project_connection",
]
