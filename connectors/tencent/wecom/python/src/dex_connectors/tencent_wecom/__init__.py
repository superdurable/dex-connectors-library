# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""WeCom group robot and application message Mutations for Dex."""

from .client import (
    Client,
    Config,
    Credentials,
    SendApplicationMessageOperation,
    SendGroupRobotMessageOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    SendApplicationMessageInput,
    SendGroupRobotMessageInput,
    SentApplicationMessage,
    SentGroupRobotMessage,
)
from .steps import new_send_application_message_step, new_send_group_robot_message_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "SendApplicationMessageInput",
    "SendApplicationMessageOperation",
    "SendGroupRobotMessageInput",
    "SendGroupRobotMessageOperation",
    "SentApplicationMessage",
    "SentGroupRobotMessage",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "new_send_application_message_step",
    "new_send_group_robot_message_step",
]
