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


# ListKnowledgeBasesInput requests the accessible CTYun Knowledge Base Q&A resources.
@dataclass(frozen=True, slots=True)
class ListKnowledgeBasesInput:
    pass


setattr(ListKnowledgeBasesInput, '__module__', 'dex_connectors.ctyun.models')


# KnowledgeBaseSummary excludes CTYun documents and private account information.
@dataclass(frozen=True, slots=True)
class KnowledgeBaseSummary:
    # ID is the provider's integer knowledge-base ID.
    id: int = 0
    # Name is the resource name.
    name: str = ''
    # FileCount is the number of files in this resource.
    file_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'fileCount'})


setattr(KnowledgeBaseSummary, '__module__', 'dex_connectors.ctyun.models')


# ListKnowledgeBasesOutput contains accessible knowledge-base resource metadata.
@dataclass(frozen=True, slots=True)
class ListKnowledgeBasesOutput:
    # KnowledgeBases are the returned resource summaries, possibly empty.
    knowledge_bases: tuple[KnowledgeBaseSummary, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'knowledgeBases'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'knowledge_bases', _replace_nil_slices(self.knowledge_bases, 1))


ListKnowledgeBasesOutput.__annotations__['knowledge_bases'] = tuple[KnowledgeBaseSummary, ...] | None
ListKnowledgeBasesOutput.__dataclass_fields__['knowledge_bases'].type = tuple[KnowledgeBaseSummary, ...] | None
setattr(ListKnowledgeBasesOutput, '__module__', 'dex_connectors.ctyun.models')
