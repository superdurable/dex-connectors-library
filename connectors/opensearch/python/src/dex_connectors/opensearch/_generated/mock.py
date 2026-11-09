# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DeleteDocumentInput, DeletedDocument, IndexDocumentInput, IndexedDocument, SearchDocumentsInput, SearchHit, SearchPage
from .definitions import CONNECTOR_ID, ABSENT, DEFECT, DELETE_DOCUMENT_DEFINITION, FOUND, INDEXED, INDEX_DOCUMENT_DEFINITION, INDEX_NOT_FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, SEARCH_DOCUMENTS_DEFINITION

def index_document_indexed(value: IndexedDocument, failure: Failure | None = None) -> Case[IndexedDocument]:
    return Case.branch_result(INDEXED, value, failure)

setattr(index_document_indexed, '__module__', "dex_connectors.opensearch.opensearchmock")

def index_document_provider_rejected(value: IndexedDocument, failure: Failure | None = None) -> Case[IndexedDocument]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(index_document_provider_rejected, '__module__', "dex_connectors.opensearch.opensearchmock")

def index_document_defect(value: IndexedDocument, failure: Failure | None = None) -> Case[IndexedDocument]:
    return Case.branch_result(DEFECT, value, failure)

setattr(index_document_defect, '__module__', "dex_connectors.opensearch.opensearchmock")

def index_document_retry(failure: Failure, after_seconds: float = 0) -> Case[IndexedDocument]:
    return Case.retry(failure, after_seconds)

setattr(index_document_retry, '__module__', "dex_connectors.opensearch.opensearchmock")

def index_document_indexed_product() -> Case[IndexedDocument]:
    return Case.branch_result(INDEXED, IndexedDocument(index='products', id='sku-42'))

setattr(index_document_indexed_product, '__module__', "dex_connectors.opensearch.opensearchmock")

def delete_document_absent(value: DeletedDocument, failure: Failure | None = None) -> Case[DeletedDocument]:
    return Case.branch_result(ABSENT, value, failure)

setattr(delete_document_absent, '__module__', "dex_connectors.opensearch.opensearchmock")

def delete_document_index_not_found(value: DeletedDocument, failure: Failure | None = None) -> Case[DeletedDocument]:
    return Case.branch_result(INDEX_NOT_FOUND, value, failure)

setattr(delete_document_index_not_found, '__module__', "dex_connectors.opensearch.opensearchmock")

def delete_document_provider_rejected(value: DeletedDocument, failure: Failure | None = None) -> Case[DeletedDocument]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(delete_document_provider_rejected, '__module__', "dex_connectors.opensearch.opensearchmock")

def delete_document_defect(value: DeletedDocument, failure: Failure | None = None) -> Case[DeletedDocument]:
    return Case.branch_result(DEFECT, value, failure)

setattr(delete_document_defect, '__module__', "dex_connectors.opensearch.opensearchmock")

def delete_document_retry(failure: Failure, after_seconds: float = 0) -> Case[DeletedDocument]:
    return Case.retry(failure, after_seconds)

setattr(delete_document_retry, '__module__', "dex_connectors.opensearch.opensearchmock")

def delete_document_deleted_product() -> Case[DeletedDocument]:
    return Case.branch_result(ABSENT, DeletedDocument(index='products', id='sku-42', was_deleted=True))

setattr(delete_document_deleted_product, '__module__', "dex_connectors.opensearch.opensearchmock")

def search_documents_found(value: SearchPage, failure: Failure | None = None) -> Case[SearchPage]:
    return Case.branch_result(FOUND, value, failure)

setattr(search_documents_found, '__module__', "dex_connectors.opensearch.opensearchmock")

def search_documents_index_not_found(value: SearchPage, failure: Failure | None = None) -> Case[SearchPage]:
    return Case.branch_result(INDEX_NOT_FOUND, value, failure)

setattr(search_documents_index_not_found, '__module__', "dex_connectors.opensearch.opensearchmock")

def search_documents_provider_rejected(value: SearchPage, failure: Failure | None = None) -> Case[SearchPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_documents_provider_rejected, '__module__', "dex_connectors.opensearch.opensearchmock")

def search_documents_invalid_response(value: SearchPage, failure: Failure | None = None) -> Case[SearchPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_documents_invalid_response, '__module__', "dex_connectors.opensearch.opensearchmock")

def search_documents_defect(value: SearchPage, failure: Failure | None = None) -> Case[SearchPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_documents_defect, '__module__', "dex_connectors.opensearch.opensearchmock")

def search_documents_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchPage]:
    return Case.retry(failure, after_seconds)

setattr(search_documents_retry, '__module__', "dex_connectors.opensearch.opensearchmock")

def search_documents_matching_document() -> Case[SearchPage]:
    return Case.branch_result(FOUND, SearchPage(total=1, relation='eq', hits=(SearchHit(index='products', id='sku-42', score=1.25, source={'title': 'Meridian', 'category': 'release'}),)))

setattr(search_documents_matching_document, '__module__', "dex_connectors.opensearch.opensearchmock")


class Mock:

    def __init__(self, connection_name: str = "opensearch") -> None:
        self.index_document_script: Mutation[IndexDocumentInput, IndexedDocument] = Mutation(INDEX_DOCUMENT_DEFINITION)
        self.index_document_script.default(index_document_indexed_product())
        self.delete_document_script: Mutation[DeleteDocumentInput, DeletedDocument] = Mutation(DELETE_DOCUMENT_DEFINITION)
        self.delete_document_script.default(delete_document_deleted_product())
        self.search_documents_script: Query[SearchDocumentsInput, SearchPage] = Query(SEARCH_DOCUMENTS_DEFINITION)
        self.search_documents_script.default(search_documents_matching_document())
        self._connection = new_connection_with_operations(
            index_document=self.index_document_script,
            delete_document=self.delete_document_script,
            search_documents=self.search_documents_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.opensearch.opensearchmock")

__all__ = [
    "Mock",
    "index_document_indexed",
    "index_document_provider_rejected",
    "index_document_defect",
    "index_document_retry",
    "index_document_indexed_product",
    "delete_document_absent",
    "delete_document_index_not_found",
    "delete_document_provider_rejected",
    "delete_document_defect",
    "delete_document_retry",
    "delete_document_deleted_product",
    "search_documents_found",
    "search_documents_index_not_found",
    "search_documents_provider_rejected",
    "search_documents_invalid_response",
    "search_documents_defect",
    "search_documents_retry",
    "search_documents_matching_document",
]
