# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Google Docs provider operations and Step factories."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    AppendTextInput,
    AppendTextOutput,
    CreatedDocument,
    CreateDocumentInput,
    CreateDocumentOutput,
    GetDocumentTextInput,
    GetDocumentTextOutput,
    PlaceholderReplacement,
    PlaceholderReplacementResult,
    ReplaceDocumentTextInput,
    ReplaceDocumentTextOutput,
)
from .steps import (
    new_append_text_step,
    new_create_document_step,
    new_get_document_text_step,
    new_replace_document_text_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "AppendTextInput",
    "AppendTextOutput",
    "CreatedDocument",
    "CreateDocumentInput",
    "CreateDocumentOutput",
    "GetDocumentTextInput",
    "GetDocumentTextOutput",
    "PlaceholderReplacement",
    "PlaceholderReplacementResult",
    "ReplaceDocumentTextInput",
    "ReplaceDocumentTextOutput",
    "new_append_text_step",
    "new_create_document_step",
    "new_get_document_text_step",
    "new_replace_document_text_step",
]
