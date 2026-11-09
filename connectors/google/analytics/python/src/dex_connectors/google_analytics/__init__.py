# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Analytics 4 operations for Dex Python Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    Audience,
    AudiencesPage,
    GetPropertyInput,
    ListAudiencesInput,
    Property,
    ReportPage,
    ReportRow,
    RunReportInput,
)
from .operations import GetPropertyOperation, ListAudiencesOperation, RunReportOperation
from .steps import new_get_property_step, new_list_audiences_step, new_run_report_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "RunReportInput",
    "ReportRow",
    "ReportPage",
    "GetPropertyInput",
    "Property",
    "ListAudiencesInput",
    "Audience",
    "AudiencesPage",
    "RunReportOperation",
    "GetPropertyOperation",
    "ListAudiencesOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "new_run_report_step",
    "new_get_property_step",
    "new_list_audiences_step",
]
