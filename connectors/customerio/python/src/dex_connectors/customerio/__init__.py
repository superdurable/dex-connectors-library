# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Customerio customer Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetCustomersByEmailOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import CustomerLookupPage, GetCustomersByEmailInput
from .steps import new_get_customers_by_email_step

__all__ = [
    "CustomerLookupPage",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetCustomersByEmailInput",
    "GetCustomersByEmailOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_customers_by_email_step",
    "new_project_connection",
]
