# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zendesk Sell Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetLeadOperation, ListLeadsOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetLeadInput, LeadMetadata, LeadsPage, ListLeadsInput
from .steps import new_get_lead_step, new_list_leads_step

__all__ = [
    "Client", "Config", "Connection", "Credentials", "LeadMetadata", "LeadsPage",
    "GetLeadInput", "GetLeadOperation", "ListLeadsInput", "ListLeadsOperation",
    "new_connection", "new_connection_with_operations",
    "new_get_lead_step", "new_list_leads_step", "new_project_connection",
]
