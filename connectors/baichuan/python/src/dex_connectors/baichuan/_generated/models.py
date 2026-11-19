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


# ListKnowledgeBasesInput requests all knowledge bases visible to the connection's API key.
@dataclass(frozen=True, slots=True)
class ListKnowledgeBasesInput:
    pass


setattr(ListKnowledgeBasesInput, '__module__', 'dex_connectors.baichuan.models')


# KnowledgeBase is the list metadata for one Baichuan knowledge base.
@dataclass(frozen=True, slots=True)
class KnowledgeBase:
    # ID is the stable provider ID used by knowledge-base APIs.
    id: str = ''
    # Name is the provider's knowledge-base name.
    name: str = ''
    # Description is the provider's knowledge-base description.
    description: str = ''
    # Status is the provider's processing state such as init, parsing, online, or fail.
    status: str = ''
    # FileIDs lists the provider file IDs attached to the knowledge base.
    file_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'fileIds'})
    # CreatedAt is the provider creation time in Unix seconds.
    created_at: int = _dataclass_field(default=0, metadata={'dex_json_name': 'createdAt'})
    # UpdatedAt is the provider update time in Unix seconds.
    updated_at: int = _dataclass_field(default=0, metadata={'dex_json_name': 'updatedAt'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'file_ids', _replace_nil_slices(self.file_ids, 1))


KnowledgeBase.__annotations__['file_ids'] = tuple[str, ...] | None
KnowledgeBase.__dataclass_fields__['file_ids'].type = tuple[str, ...] | None
setattr(KnowledgeBase, '__module__', 'dex_connectors.baichuan.models')


# ListKnowledgeBasesOutput contains all knowledge bases returned by Baichuan's unpaginated list API.
@dataclass(frozen=True, slots=True)
class ListKnowledgeBasesOutput:
    # KnowledgeBases is empty when the provider returned no knowledge bases.
    knowledge_bases: tuple[KnowledgeBase, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'knowledgeBases'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'knowledge_bases', _replace_nil_slices(self.knowledge_bases, 1))


ListKnowledgeBasesOutput.__annotations__['knowledge_bases'] = tuple[KnowledgeBase, ...] | None
ListKnowledgeBasesOutput.__dataclass_fields__['knowledge_bases'].type = tuple[KnowledgeBase, ...] | None
setattr(ListKnowledgeBasesOutput, '__module__', 'dex_connectors.baichuan.models')
