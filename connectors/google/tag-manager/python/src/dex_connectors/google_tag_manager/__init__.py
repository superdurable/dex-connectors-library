# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Tag Manager operations for Dex Python Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    AccountsPage,
    ContainersPage,
    ListAccountsInput,
    ListContainersInput,
    ListTagsInput,
    TagManagerAccount,
    TagManagerContainer,
    TagManagerTag,
    TagsPage,
)
from .operations import ListAccountsOperation, ListContainersOperation, ListTagsOperation
from .steps import new_list_accounts_step, new_list_containers_step, new_list_tags_step

__all__ = [
    "AccountsPage",
    "Client",
    "Config",
    "Connection",
    "ContainersPage",
    "Credentials",
    "ListAccountsInput",
    "ListAccountsOperation",
    "ListContainersInput",
    "ListContainersOperation",
    "ListTagsInput",
    "ListTagsOperation",
    "TagManagerAccount",
    "TagManagerContainer",
    "TagManagerTag",
    "TagsPage",
    "new_connection",
    "new_connection_with_operations",
    "new_list_accounts_step",
    "new_list_containers_step",
    "new_list_tags_step",
    "new_project_connection",
]
