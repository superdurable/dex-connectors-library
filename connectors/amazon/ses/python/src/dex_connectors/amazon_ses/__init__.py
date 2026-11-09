# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Amazon SES key Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetEmailIdentityOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import EmailIdentityDetails, GetEmailIdentityInput
from .steps import new_get_email_identity_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetEmailIdentityInput",
    "EmailIdentityDetails",
    "GetEmailIdentityOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_email_identity_step",
    "new_project_connection",
]
