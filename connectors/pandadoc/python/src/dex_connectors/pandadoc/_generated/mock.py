# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DocumentStatus, GetDocumentInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_DOCUMENT_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_document_found(value: DocumentStatus, failure: Failure | None = None) -> Case[DocumentStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_document_found, '__module__', "dex_connectors.pandadoc.pandadocmock")

def get_document_not_found(value: DocumentStatus, failure: Failure | None = None) -> Case[DocumentStatus]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_document_not_found, '__module__', "dex_connectors.pandadoc.pandadocmock")

def get_document_provider_rejected(value: DocumentStatus, failure: Failure | None = None) -> Case[DocumentStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_document_provider_rejected, '__module__', "dex_connectors.pandadoc.pandadocmock")

def get_document_invalid_response(value: DocumentStatus, failure: Failure | None = None) -> Case[DocumentStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_document_invalid_response, '__module__', "dex_connectors.pandadoc.pandadocmock")

def get_document_defect(value: DocumentStatus, failure: Failure | None = None) -> Case[DocumentStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_document_defect, '__module__', "dex_connectors.pandadoc.pandadocmock")

def get_document_retry(failure: Failure, after_seconds: float = 0) -> Case[DocumentStatus]:
    return Case.retry(failure, after_seconds)

setattr(get_document_retry, '__module__', "dex_connectors.pandadoc.pandadocmock")

def get_document_found_default() -> Case[DocumentStatus]:
    return Case.branch_result(FOUND, DocumentStatus(id='fANp5wW5EwY7AkiTkTGekZ', name='Example agreement', status='document.completed'))

setattr(get_document_found_default, '__module__', "dex_connectors.pandadoc.pandadocmock")


class Mock:

    def __init__(self, connection_name: str = "pandadoc") -> None:
        self.get_document_script: Query[GetDocumentInput, DocumentStatus] = Query(GET_DOCUMENT_DEFINITION)
        self.get_document_script.default(get_document_found_default())
        self._connection = new_connection_with_operations(
            get_document=self.get_document_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.pandadoc.pandadocmock")

__all__ = [
    "Mock",
    "get_document_found",
    "get_document_not_found",
    "get_document_provider_rejected",
    "get_document_invalid_response",
    "get_document_defect",
    "get_document_retry",
    "get_document_found_default",
]
