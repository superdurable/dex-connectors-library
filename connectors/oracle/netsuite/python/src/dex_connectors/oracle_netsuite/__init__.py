# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Oracle NetSuite operations for Dex Python Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import CustomerSummary, GetCustomerInput
from .operations import GetCustomerOperation
from .steps import new_get_customer_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetCustomerInput",
    "GetCustomerOperation",
    "CustomerSummary",
    "new_connection",
    "new_connection_with_operations",
    "new_get_customer_step",
    "new_project_connection",
]
