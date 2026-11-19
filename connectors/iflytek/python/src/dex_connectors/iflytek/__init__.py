# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""iFLYTEK Spark chat completion Query for Dex Flows."""

from .client import Client, Config, CreateChatCompletionOperation, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import ChatCompletion, CreateChatCompletionInput
from .steps import new_create_chat_completion_step

__all__ = [
    "Client", "Config", "Credentials", "Connection", "ChatCompletion",
    "CreateChatCompletionInput", "CreateChatCompletionOperation", "new_connection",
    "new_connection_with_operations", "new_create_chat_completion_step", "new_project_connection",
]
