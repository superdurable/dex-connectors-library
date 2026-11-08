# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Copper Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetLeadOperation, ListLeadStatusesOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetLeadInput, LeadMetadata, LeadStatusesList, LeadStatusMetadata, ListLeadStatusesInput
from .steps import new_get_lead_step, new_list_lead_statuses_step

__all__ = [
    "Client", "Config", "Connection", "Credentials", "LeadStatusMetadata", "LeadStatusesList",
    "GetLeadInput", "GetLeadOperation", "ListLeadStatusesInput", "ListLeadStatusesOperation",
    "LeadMetadata", "new_connection", "new_connection_with_operations",
    "new_get_lead_step", "new_list_lead_statuses_step", "new_project_connection",
]
