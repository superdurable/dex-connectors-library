# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Meta Ads Insights ad Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetAdOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import AdMetadata, GetAdInput
from .steps import new_get_ad_step

__all__ = [
    "AdMetadata",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetAdInput",
    "GetAdOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_ad_step",
    "new_project_connection",
]
