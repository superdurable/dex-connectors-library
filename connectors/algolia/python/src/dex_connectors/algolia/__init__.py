# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Algolia index search and record replacement operations for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    PUBLIC_HOST_TEMPLATE,
    SAVE_OBJECT_DEFINITION,
    SAVED,
    SEARCH_DEFINITION,
    SEARCHED,
    Client,
    Config,
    Credentials,
    SaveObjectOperation,
    SearchOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import SavedObject, SaveObjectInput, SearchHit, SearchInput, SearchPage
from .steps import new_save_object_step, new_search_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "SaveObjectInput",
    "SaveObjectOperation",
    "SavedObject",
    "SearchHit",
    "SearchInput",
    "SearchOperation",
    "SearchPage",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "new_save_object_step",
    "new_search_step",
    "CONNECTOR_ID",
    "DEFECT",
    "INVALID_RESPONSE",
    "PROVIDER_REJECTED",
    "PUBLIC_HOST_TEMPLATE",
    "SAVE_OBJECT_DEFINITION",
    "SAVED",
    "SEARCH_DEFINITION",
    "SEARCHED",
]
