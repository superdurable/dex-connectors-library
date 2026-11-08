# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Document, DocumentsPage, ListDocumentsInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_DOCUMENTS_DEFINITION, PROVIDER_REJECTED

def list_documents_listed(value: DocumentsPage, failure: Failure | None = None) -> Case[DocumentsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_documents_listed, '__module__', "dex_connectors.google_firestore.firestoremock")

def list_documents_provider_rejected(value: DocumentsPage, failure: Failure | None = None) -> Case[DocumentsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_documents_provider_rejected, '__module__', "dex_connectors.google_firestore.firestoremock")

def list_documents_invalid_response(value: DocumentsPage, failure: Failure | None = None) -> Case[DocumentsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_documents_invalid_response, '__module__', "dex_connectors.google_firestore.firestoremock")

def list_documents_defect(value: DocumentsPage, failure: Failure | None = None) -> Case[DocumentsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_documents_defect, '__module__', "dex_connectors.google_firestore.firestoremock")

def list_documents_retry(failure: Failure, after_seconds: float = 0) -> Case[DocumentsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_documents_retry, '__module__', "dex_connectors.google_firestore.firestoremock")

def list_documents_listed_default() -> Case[DocumentsPage]:
    return Case.branch_result(LISTED, DocumentsPage(documents=(Document(name='projects/sample-project/databases/(default)/documents/events/sample-event'),), next_page_token=''))

setattr(list_documents_listed_default, '__module__', "dex_connectors.google_firestore.firestoremock")


class Mock:

    def __init__(self, connection_name: str = "google-firestore") -> None:
        self.list_documents_script: Query[ListDocumentsInput, DocumentsPage] = Query(LIST_DOCUMENTS_DEFINITION)
        self.list_documents_script.default(list_documents_listed_default())
        self._connection = new_connection_with_operations(
            list_documents=self.list_documents_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_firestore.firestoremock")

__all__ = [
    "Mock",
    "list_documents_listed",
    "list_documents_provider_rejected",
    "list_documents_invalid_response",
    "list_documents_defect",
    "list_documents_retry",
    "list_documents_listed_default",
]
