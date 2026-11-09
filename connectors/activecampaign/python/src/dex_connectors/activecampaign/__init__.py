# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ActiveCampaign Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetContactOperation, ListContactsOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import ContactMetadata, ContactsPage, GetContactInput, ListContactsInput
from .steps import new_get_contact_step, new_list_contacts_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "ContactMetadata",
    "ContactsPage",
    "GetContactInput",
    "GetContactOperation",
    "ListContactsInput",
    "ListContactsOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_contact_step",
    "new_list_contacts_step",
    "new_project_connection",
]
