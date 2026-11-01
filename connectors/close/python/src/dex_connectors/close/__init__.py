# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Close lead operations for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_LEAD_DEFINITION,
    INVALID_RESPONSE,
    LIST_LEADS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetLeadOperation,
    ListLeadsOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetLeadInput, Lead, LeadPage, ListLeadsInput
from .steps import new_get_lead_step, new_list_leads_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetLeadInput",
    "GetLeadOperation",
    "ListLeadsInput",
    "ListLeadsOperation",
    "Lead",
    "LeadPage",
    "new_connection",
    "new_connection_with_operations",
    "new_get_lead_step",
    "new_list_leads_step",
    "new_project_connection",
    "CONNECTOR_ID",
    "DEFECT",
    "FOUND",
    "GET_LEAD_DEFINITION",
    "INVALID_RESPONSE",
    "LIST_LEADS_DEFINITION",
    "LISTED",
    "NOT_FOUND",
    "PROVIDER_REJECTED",
]
