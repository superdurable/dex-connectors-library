# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""NetEase Yunxin user profile Query for Dex Flows."""

from .client import Client, Config, Credentials, GetUserProfileOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetUserProfileInput, GetUserProfileOutput
from .steps import new_get_user_profile_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "GetUserProfileInput",
    "GetUserProfileOutput",
    "GetUserProfileOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_user_profile_step",
    "new_project_connection",
]
