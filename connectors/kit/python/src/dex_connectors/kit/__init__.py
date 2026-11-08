# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Kit Queries for Dex Flows."""

from .client import Client, Config, Credentials, GetSubscriberOperation, ListSubscribersOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetSubscriberInput, ListSubscribersInput, SubscriberMetadata, SubscribersPage
from .steps import new_get_subscriber_step, new_list_subscribers_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "SubscriberMetadata",
    "SubscribersPage",
    "GetSubscriberInput",
    "GetSubscriberOperation",
    "ListSubscribersInput",
    "ListSubscribersOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_subscriber_step",
    "new_list_subscribers_step",
    "new_project_connection",
]
