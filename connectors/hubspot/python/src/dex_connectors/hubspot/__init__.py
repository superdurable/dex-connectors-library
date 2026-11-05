# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""HubSpot CRM connector for Dex Python applications."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    CRMObject,
    GetObjectInput,
    ObjectPage,
    SearchFilter,
    SearchFilterGroup,
    SearchObjectsInput,
    SearchSort,
    UpdateObjectInput,
    UpsertedObject,
    UpsertObjectInput,
)
from .steps import (
    new_get_object_step,
    new_search_objects_step,
    new_update_object_step,
    new_upsert_object_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "CRMObject",
    "GetObjectInput",
    "ObjectPage",
    "SearchFilter",
    "SearchFilterGroup",
    "SearchObjectsInput",
    "SearchSort",
    "UpdateObjectInput",
    "UpsertObjectInput",
    "UpsertedObject",
    "new_get_object_step",
    "new_search_objects_step",
    "new_update_object_step",
    "new_upsert_object_step",
]
