# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Contacts operations for Dex Python Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import Contact, ContactsPage, CreateContactInput, GetContactInput, ListContactsInput
from .operations import CreateContactOperation, GetContactOperation, ListContactsOperation
from .steps import new_create_contact_step, new_get_contact_step, new_list_contacts_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Contact",
    "ContactsPage",
    "CreateContactInput",
    "CreateContactOperation",
    "Credentials",
    "GetContactInput",
    "GetContactOperation",
    "ListContactsInput",
    "ListContactsOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_create_contact_step",
    "new_get_contact_step",
    "new_list_contacts_step",
    "new_project_connection",
]
