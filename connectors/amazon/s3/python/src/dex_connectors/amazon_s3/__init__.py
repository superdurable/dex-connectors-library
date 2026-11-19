# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Amazon S3 connector operations and typed Dex Steps."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    GetObjectTextInput,
    HeadObjectInput,
    ListObjectsInput,
    ListObjectsOutput,
    ObjectMetadata,
    ObjectSummary,
    ObjectText,
    PutObjectInput,
    StoredObject,
)
from .steps import (
    new_get_object_text_step,
    new_head_object_step,
    new_list_objects_step,
    new_put_object_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "GetObjectTextInput",
    "HeadObjectInput",
    "ListObjectsInput",
    "ListObjectsOutput",
    "ObjectMetadata",
    "ObjectSummary",
    "ObjectText",
    "PutObjectInput",
    "StoredObject",
    "new_get_object_text_step",
    "new_head_object_step",
    "new_list_objects_step",
    "new_put_object_step",
]
