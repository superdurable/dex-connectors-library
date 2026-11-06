# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Workspace Admin Directory operations for Dex Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    AddUserToGroupInput,
    AddUserToGroupOutput,
    CreateUserInput,
    CreateUserOutput,
    GetUserInput,
    GroupMembership,
    ListUsersInput,
    ListUsersOutput,
    RemoveUserFromGroupInput,
    RemoveUserFromGroupOutput,
    SuspendUserInput,
    UnsuspendUserInput,
    User,
    UserName,
)
from .operations import (
    AddUserToGroupOperation,
    CreateUserOperation,
    GetUserOperation,
    ListUsersOperation,
    RemoveUserFromGroupOperation,
    SuspendUserOperation,
    UnsuspendUserOperation,
)
from .steps import (
    new_add_user_to_group_step,
    new_create_user_step,
    new_get_user_step,
    new_list_users_step,
    new_remove_user_from_group_step,
    new_suspend_user_step,
    new_unsuspend_user_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "AddUserToGroupInput",
    "AddUserToGroupOutput",
    "CreateUserInput",
    "CreateUserOutput",
    "GetUserInput",
    "GroupMembership",
    "ListUsersInput",
    "ListUsersOutput",
    "RemoveUserFromGroupInput",
    "RemoveUserFromGroupOutput",
    "SuspendUserInput",
    "UnsuspendUserInput",
    "User",
    "UserName",
    "AddUserToGroupOperation",
    "CreateUserOperation",
    "GetUserOperation",
    "ListUsersOperation",
    "RemoveUserFromGroupOperation",
    "SuspendUserOperation",
    "UnsuspendUserOperation",
    "new_add_user_to_group_step",
    "new_create_user_step",
    "new_get_user_step",
    "new_list_users_step",
    "new_remove_user_from_group_step",
    "new_suspend_user_step",
    "new_unsuspend_user_step",
]
