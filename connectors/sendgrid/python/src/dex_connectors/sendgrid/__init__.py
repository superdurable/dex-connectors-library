# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""SendGrid statistics and template operations for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_STATS_DEFINITION,
    INVALID_RESPONSE,
    LIST_TEMPLATES_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
    TEMPLATE_GENERATIONS,
    Client,
    Config,
    Credentials,
    GetStatsOperation,
    ListTemplatesOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetStatsInput, ListTemplatesInput, StatsDay, StatsOutput, Template, TemplatesOutput
from .steps import new_get_stats_step, new_list_templates_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetStatsInput",
    "GetStatsOperation",
    "ListTemplatesInput",
    "ListTemplatesOperation",
    "StatsDay",
    "StatsOutput",
    "Template",
    "TemplatesOutput",
    "new_connection",
    "new_connection_with_operations",
    "new_get_stats_step",
    "new_list_templates_step",
    "new_project_connection",
    "CONNECTOR_ID",
    "DEFECT",
    "FOUND",
    "GET_STATS_DEFINITION",
    "INVALID_RESPONSE",
    "LIST_TEMPLATES_DEFINITION",
    "LISTED",
    "PROVIDER_REJECTED",
    "TEMPLATE_GENERATIONS",
]
