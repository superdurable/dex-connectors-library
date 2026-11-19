# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""FastGPT knowledge base details Query for Dex Flows."""

from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_DATASET_DETAILS_DEFINITION,
    GET_DATASET_DETAILS_OPERATION_ID,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
)
from .client import Client, Config, Credentials, GetDatasetDetailsOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetDatasetDetailsInput, GetDatasetDetailsOutput
from .steps import new_get_dataset_details_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "GetDatasetDetailsOperation",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "GetDatasetDetailsInput",
    "GetDatasetDetailsOutput",
    "new_get_dataset_details_step",
    "CONNECTOR_ID",
    "DEFECT",
    "FOUND",
    "INVALID_RESPONSE",
    "PROVIDER_REJECTED",
    "GET_DATASET_DETAILS_DEFINITION",
    "GET_DATASET_DETAILS_OPERATION_ID",
]
