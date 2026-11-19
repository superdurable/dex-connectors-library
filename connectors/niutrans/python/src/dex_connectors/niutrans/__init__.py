# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Niutrans connector for Dex Flows."""

from .client import Client, Config, Credentials, TranslateTextOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import TranslateTextInput, TranslateTextOutput
from .steps import new_translate_text_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "TranslateTextOperation",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "TranslateTextInput",
    "TranslateTextOutput",
    "new_translate_text_step",
]
