# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider-neutral text generation connector for Dex."""

from . import models as _models
from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GenerateTextInput
from .steps import new_generate_text_step

# SDK-owned aliases stay available for explicit imports without claiming connector ownership in __all__.
GenerateTextRequest = _models.GenerateTextRequest
GenerateTextResponse = _models.GenerateTextResponse
Message = _models.Message
StructuredOutput = _models.StructuredOutput
Usage = _models.Usage

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "GenerateTextInput",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "new_generate_text_step",
]
