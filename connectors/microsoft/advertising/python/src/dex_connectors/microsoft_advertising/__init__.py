# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""MicrosoftAdvertising operations for Dex Python Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import CampaignsPage, CampaignSummary, ListCampaignsInput
from .operations import ListCampaignsOperation
from .steps import new_list_campaigns_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "CampaignSummary",
    "ListCampaignsInput",
    "ListCampaignsOperation",
    "CampaignsPage",
    "new_connection",
    "new_connection_with_operations",
    "new_list_campaigns_step",
    "new_project_connection",
]
