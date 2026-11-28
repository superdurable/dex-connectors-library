# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Personio person Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetPersonOperation, ListPersonsOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetPersonInput, ListPersonsInput, PersonPage, PersonProfile
from .steps import new_get_person_step, new_list_persons_step

__all__ = [
    "PersonProfile",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetPersonInput",
    "GetPersonOperation",
    "ListPersonsInput",
    "PersonPage",
    "ListPersonsOperation",
    "new_list_persons_step",
    "new_connection",
    "new_connection_with_operations",
    "new_get_person_step",
    "new_project_connection",
]
