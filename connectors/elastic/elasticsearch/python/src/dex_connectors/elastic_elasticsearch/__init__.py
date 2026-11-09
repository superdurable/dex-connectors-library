# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Elasticsearch index operations for Dex Python Flows."""

from .client import (
    Client,
    Config,
    Credentials,
    DeleteDocumentOperation,
    IndexDocumentOperation,
    SearchDocumentsOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    DeletedDocument,
    DeleteDocumentInput,
    IndexDocumentInput,
    IndexedDocument,
    SearchDocumentsInput,
    SearchHit,
    SearchPage,
)
from .steps import new_delete_document_step, new_index_document_step, new_search_documents_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "IndexDocumentOperation",
    "DeleteDocumentOperation",
    "SearchDocumentsOperation",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "DeleteDocumentInput",
    "DeletedDocument",
    "IndexDocumentInput",
    "IndexedDocument",
    "SearchDocumentsInput",
    "SearchHit",
    "SearchPage",
    "new_index_document_step",
    "new_delete_document_step",
    "new_search_documents_step",
]
