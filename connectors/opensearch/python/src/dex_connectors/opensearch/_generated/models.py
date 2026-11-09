# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# IndexDocumentInput fully replaces one document under a stable caller-selected ID.
@dataclass(frozen=True, slots=True)
class IndexDocumentInput:
    # Index is one concrete lowercase OpenSearch index name.
    index: str = ''
    # DocumentID is the stable ID used for every retry.
    document_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'documentId'})
    # Source is a complete JSON object replacing the current document.
    source: Any = None


setattr(IndexDocumentInput, '__module__', 'dex_connectors.opensearch.models')


# IndexedDocument identifies the document stored by indexDocument.
@dataclass(frozen=True, slots=True)
class IndexedDocument:
    # Index is the index that contains the document.
    index: str = ''
    # ID is the stable document ID.
    id: str = ''


setattr(IndexedDocument, '__module__', 'dex_connectors.opensearch.models')


# DeleteDocumentInput identifies one document to remove.
@dataclass(frozen=True, slots=True)
class DeleteDocumentInput:
    # Index is one concrete lowercase OpenSearch index name.
    index: str = ''
    # DocumentID is the stable ID to remove.
    document_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'documentId'})


setattr(DeleteDocumentInput, '__module__', 'dex_connectors.opensearch.models')


# DeletedDocument reports that the target document is absent.
@dataclass(frozen=True, slots=True)
class DeletedDocument:
    # Index is the index containing the target ID.
    index: str = ''
    # ID is the stable target document ID.
    id: str = ''
    # WasDeleted reports whether this attempt removed an existing document.
    was_deleted: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasDeleted'})


setattr(DeletedDocument, '__module__', 'dex_connectors.opensearch.models')


# SearchDocumentsInput selects an index, Lucene query, and bounded offset page.
@dataclass(frozen=True, slots=True)
class SearchDocumentsInput:
    # Index is one concrete lowercase OpenSearch index name.
    index: str = ''
    # Query is a Lucene query string, such as title:Meridian.
    query: str = ''
    # Size is 1 through 100; zero uses ten results.
    size: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # From is a zero-based offset; from plus size may not exceed 10000.
    from_: int = _dataclass_field(default=0, metadata={'dex_json_name': 'from', 'dex_json_omit_default': True})


setattr(SearchDocumentsInput, '__module__', 'dex_connectors.opensearch.models')


# SearchHit identifies one matching document and its untrusted source JSON.
@dataclass(frozen=True, slots=True)
class SearchHit:
    # Index is the index that produced this hit.
    index: str = ''
    # ID is the provider document ID.
    id: str = ''
    # Score is OpenSearch's relevance score, if available.
    score: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Source is provider-owned document JSON and may contain sensitive user data.
    source: Any = None


setattr(SearchHit, '__module__', 'dex_connectors.opensearch.models')


# SearchPage is one bounded search result page.
@dataclass(frozen=True, slots=True)
class SearchPage:
    # Total is the provider's matching hit count or lower bound.
    total: int = 0
    # Relation is eq for an exact count or gte for a lower bound.
    relation: str = ''
    # Hits contains this page's matching documents.
    hits: tuple[SearchHit, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'hits', _replace_nil_slices(self.hits, 1))


SearchPage.__annotations__['hits'] = tuple[SearchHit, ...] | None
SearchPage.__dataclass_fields__['hits'].type = tuple[SearchHit, ...] | None
setattr(SearchPage, '__module__', 'dex_connectors.opensearch.models')
