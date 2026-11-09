# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deel contract Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetContractOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import ContractStatus, GetContractInput
from .steps import new_get_contract_step

__all__ = [
    "ContractStatus",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetContractInput",
    "GetContractOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_contract_step",
    "new_project_connection",
]
