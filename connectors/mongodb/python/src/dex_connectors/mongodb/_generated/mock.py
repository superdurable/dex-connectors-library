# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DocumentSet, FindDocumentsInput
from .definitions import CONNECTOR_ID, DEFECT, FIND_DOCUMENTS_DEFINITION, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, TRUNCATED

def find_documents_found(value: DocumentSet, failure: Failure | None = None) -> Case[DocumentSet]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_documents_found, '__module__', "dex_connectors.mongodb.mongodbmock")

def find_documents_truncated(value: DocumentSet, failure: Failure | None = None) -> Case[DocumentSet]:
    return Case.branch_result(TRUNCATED, value, failure)

setattr(find_documents_truncated, '__module__', "dex_connectors.mongodb.mongodbmock")

def find_documents_provider_rejected(value: DocumentSet, failure: Failure | None = None) -> Case[DocumentSet]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_documents_provider_rejected, '__module__', "dex_connectors.mongodb.mongodbmock")

def find_documents_invalid_response(value: DocumentSet, failure: Failure | None = None) -> Case[DocumentSet]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_documents_invalid_response, '__module__', "dex_connectors.mongodb.mongodbmock")

def find_documents_defect(value: DocumentSet, failure: Failure | None = None) -> Case[DocumentSet]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_documents_defect, '__module__', "dex_connectors.mongodb.mongodbmock")

def find_documents_retry(failure: Failure, after_seconds: float = 0) -> Case[DocumentSet]:
    return Case.retry(failure, after_seconds)

setattr(find_documents_retry, '__module__', "dex_connectors.mongodb.mongodbmock")

def find_documents_found_default() -> Case[DocumentSet]:
    return Case.branch_result(FOUND, DocumentSet(documents=({'_id': {'$oid': '507f1f77bcf86cd799439011'}, 'status': 'active'},)))

setattr(find_documents_found_default, '__module__', "dex_connectors.mongodb.mongodbmock")


class Mock:

    def __init__(self, connection_name: str = "mongodb") -> None:
        self.find_documents_script: Query[FindDocumentsInput, DocumentSet] = Query(FIND_DOCUMENTS_DEFINITION)
        self.find_documents_script.default(find_documents_found_default())
        self._connection = new_connection_with_operations(
            find_documents=self.find_documents_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.mongodb.mongodbmock")

__all__ = [
    "Mock",
    "find_documents_found",
    "find_documents_truncated",
    "find_documents_provider_rejected",
    "find_documents_invalid_response",
    "find_documents_defect",
    "find_documents_retry",
    "find_documents_found_default",
]
