# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# FindDocumentsInput names one collection and supplies a JSON query filter.
@dataclass(frozen=True, slots=True)
class FindDocumentsInput:
    # Collection is a non-secret name in the configured database.
    collection: str = ''
    # Filter is a JSON object; MongoDB Extended JSON values preserve BSON types.
    filter: dict[str, Any] | None = None


setattr(FindDocumentsInput, '__module__', 'dex_connectors.mongodb.models')


# DocumentSet contains bounded canonical Extended JSON documents.
@dataclass(frozen=True, slots=True)
class DocumentSet:
    # Documents contains JSON objects with BSON types represented by Extended JSON wrappers.
    documents: tuple[dict[str, Any] | None, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'documents', _replace_nil_slices(self.documents, 1))


DocumentSet.__annotations__['documents'] = tuple[dict[str, Any] | None, ...] | None
DocumentSet.__dataclass_fields__['documents'].type = tuple[dict[str, Any] | None, ...] | None
setattr(DocumentSet, '__module__', 'dex_connectors.mongodb.models')
