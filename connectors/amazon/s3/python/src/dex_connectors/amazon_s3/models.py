# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed S3 inputs and outputs generated from the Go connector contract."""

from ._generated.models import (
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

__all__ = [
    "GetObjectTextInput",
    "HeadObjectInput",
    "ListObjectsInput",
    "ListObjectsOutput",
    "ObjectMetadata",
    "ObjectSummary",
    "ObjectText",
    "PutObjectInput",
    "StoredObject",
]
