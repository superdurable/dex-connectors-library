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


# ListDocumentsInput selects a page by its opaque, non-secret token.
@dataclass(frozen=True, slots=True)
class ListDocumentsInput:
    # PageToken is blank for the first page or a previous NextPageToken value.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListDocumentsInput, '__module__', 'dex_connectors.lucid.models')


# DocumentMetadata contains only the document ID and title.
@dataclass(frozen=True, slots=True)
class DocumentMetadata:
    # ID is the Lucid document UUID.
    id: str = ''
    # Title is the document title.
    title: str = ''


setattr(DocumentMetadata, '__module__', 'dex_connectors.lucid.models')


# DocumentsPage contains one page and the next opaque page token, if any.
@dataclass(frozen=True, slots=True)
class DocumentsPage:
    # Documents excludes content, owners, URLs, and custom attributes.
    documents: tuple[DocumentMetadata, ...] = ()
    # NextPageToken is blank at the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'documents', _replace_nil_slices(self.documents, 1))


DocumentsPage.__annotations__['documents'] = tuple[DocumentMetadata, ...] | None
DocumentsPage.__dataclass_fields__['documents'].type = tuple[DocumentMetadata, ...] | None
setattr(DocumentsPage, '__module__', 'dex_connectors.lucid.models')
