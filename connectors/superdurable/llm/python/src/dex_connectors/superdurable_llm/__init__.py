# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider-neutral text generation connector for Dex."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    GenerateTextInput,
    GenerateTextRequest,
    GenerateTextResponse,
    Message,
    StructuredOutput,
    Usage,
)
from .steps import new_generate_text_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "GenerateTextInput",
    "GenerateTextRequest",
    "GenerateTextResponse",
    "Message",
    "StructuredOutput",
    "Usage",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "new_generate_text_step",
]
