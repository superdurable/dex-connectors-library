# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""DataEyes public web page reader for Dex Flows."""

from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    READ_WEB_PAGE_DEFINITION,
    READ_WEB_PAGE_OPERATION_ID,
)
from .client import Client, Config, Credentials, ReadWebPageOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import ReadWebPageInput, ReadWebPageOutput
from .steps import new_read_web_page_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "ReadWebPageOperation",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "ReadWebPageInput",
    "ReadWebPageOutput",
    "new_read_web_page_step",
    "CONNECTOR_ID",
    "DEFECT",
    "FOUND",
    "INVALID_RESPONSE",
    "PROVIDER_REJECTED",
    "READ_WEB_PAGE_DEFINITION",
    "READ_WEB_PAGE_OPERATION_ID",
]
