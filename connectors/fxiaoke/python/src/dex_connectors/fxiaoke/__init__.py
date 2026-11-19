# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""FXiaoke CRM object metadata Query for Dex Flows."""

from .client import Client, Config, Credentials, ListObjectMetadataOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import ListObjectMetadataInput, ListObjectMetadataOutput, ObjectMetadata
from .steps import new_list_object_metadata_step

__all__ = [
    "Client", "Config", "Credentials", "ListObjectMetadataOperation", "Connection",
    "ListObjectMetadataInput", "ListObjectMetadataOutput", "ObjectMetadata", "new_connection",
    "new_connection_with_operations", "new_project_connection", "new_list_object_metadata_step",
]
