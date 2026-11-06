# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public Google Docs model facade."""

from ._generated.models import (
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

__all__ = [
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
]
