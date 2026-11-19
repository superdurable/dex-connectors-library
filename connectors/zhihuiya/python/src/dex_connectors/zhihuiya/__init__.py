# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zhihuiya Patent Valuation connector for Dex Flows."""

from .client import Client, Config, Credentials, GetPatentValuationOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetPatentValuationInput, GetPatentValuationOutput
from .steps import new_get_patent_valuation_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetPatentValuationInput",
    "GetPatentValuationOperation",
    "GetPatentValuationOutput",
    "new_connection",
    "new_connection_with_operations",
    "new_get_patent_valuation_step",
    "new_project_connection",
]
