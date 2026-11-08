# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Odoo company partner search Query for Dex Flows."""

from .client import Client, Config, Credentials, SearchPartnersOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import Partner, PartnerSearchPage, SearchPartnersInput
from .steps import new_search_partners_step

__all__ = [
    "PartnerSearchPage",
    "Partner",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "SearchPartnersInput",
    "SearchPartnersOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_search_partners_step",
    "new_project_connection",
]
