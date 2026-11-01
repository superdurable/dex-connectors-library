# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Brevo contact operations for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    CREATE_OR_UPDATE_CONTACT_DEFINITION,
    DEFECT,
    FOUND,
    GET_CONTACT_DEFINITION,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
    SAVED,
    Client,
    Config,
    CreateOrUpdateContactOperation,
    Credentials,
    GetContactOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import Contact, ContactWriteResult, CreateOrUpdateContactInput, GetContactInput
from .steps import new_create_or_update_contact_step, new_get_contact_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Contact",
    "ContactWriteResult",
    "CreateOrUpdateContactInput",
    "CreateOrUpdateContactOperation",
    "Credentials",
    "GetContactInput",
    "GetContactOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_create_or_update_contact_step",
    "new_get_contact_step",
    "new_project_connection",
    "CONNECTOR_ID",
    "CREATE_OR_UPDATE_CONTACT_DEFINITION",
    "DEFECT",
    "FOUND",
    "GET_CONTACT_DEFINITION",
    "INVALID_RESPONSE",
    "NOT_FOUND",
    "PROVIDER_REJECTED",
    "SAVED",
]
