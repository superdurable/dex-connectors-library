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


# ListDocumentsInput selects one bounded collection page in the default Firestore database.
@dataclass(frozen=True, slots=True)
class ListDocumentsInput:
    # ProjectID is the Google Cloud project ID shown in Firestore Databases.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId'})
    # CollectionID is the top-level collection ID shown in Firestore Documents.
    collection_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'collectionId'})
    # PageSize limits this page to 1 through 100 documents; zero uses 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is returned by the previous page; blank selects the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListDocumentsInput, '__module__', 'dex_connectors.google_firestore.models')


# Document identifies a Firestore document by its full resource name.
@dataclass(frozen=True, slots=True)
class Document:
    # Name has the form projects/{project-id}/databases/(default)/documents/{collection-id}/{document-id}.
    name: str = ''


setattr(Document, '__module__', 'dex_connectors.google_firestore.models')


# DocumentsPage holds one page of document names and its next cursor.
@dataclass(frozen=True, slots=True)
class DocumentsPage:
    # Documents are visible to the authorized Google identity.
    documents: tuple[Document, ...] = ()
    # NextPageToken is blank on the final page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'documents', _replace_nil_slices(self.documents, 1))


DocumentsPage.__annotations__['documents'] = tuple[Document, ...] | None
DocumentsPage.__dataclass_fields__['documents'].type = tuple[Document, ...] | None
setattr(DocumentsPage, '__module__', 'dex_connectors.google_firestore.models')
