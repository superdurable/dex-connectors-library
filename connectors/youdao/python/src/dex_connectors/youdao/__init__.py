# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Youdao connector for Dex Flows."""

from .client import Client, Config, Credentials, TranslateTextOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import TextTranslation, TranslateTextInput
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
    "TextTranslation",
    "new_translate_text_step",
]
