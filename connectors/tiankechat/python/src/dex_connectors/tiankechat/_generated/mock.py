# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import KnowledgeEntry, ListKnowledgeEntriesInput, ListKnowledgeEntriesOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_KNOWLEDGE_ENTRIES_DEFINITION, PROVIDER_REJECTED

def list_knowledge_entries_listed(value: ListKnowledgeEntriesOutput, failure: Failure | None = None) -> Case[ListKnowledgeEntriesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_knowledge_entries_listed, '__module__', "dex_connectors.tiankechat.tiankechatmock")

def list_knowledge_entries_provider_rejected(value: ListKnowledgeEntriesOutput, failure: Failure | None = None) -> Case[ListKnowledgeEntriesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_knowledge_entries_provider_rejected, '__module__', "dex_connectors.tiankechat.tiankechatmock")

def list_knowledge_entries_invalid_response(value: ListKnowledgeEntriesOutput, failure: Failure | None = None) -> Case[ListKnowledgeEntriesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_knowledge_entries_invalid_response, '__module__', "dex_connectors.tiankechat.tiankechatmock")

def list_knowledge_entries_defect(value: ListKnowledgeEntriesOutput, failure: Failure | None = None) -> Case[ListKnowledgeEntriesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_knowledge_entries_defect, '__module__', "dex_connectors.tiankechat.tiankechatmock")

def list_knowledge_entries_retry(failure: Failure, after_seconds: float = 0) -> Case[ListKnowledgeEntriesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_knowledge_entries_retry, '__module__', "dex_connectors.tiankechat.tiankechatmock")

def list_knowledge_entries_sample_knowledge() -> Case[ListKnowledgeEntriesOutput]:
    return Case.branch_result(LISTED, ListKnowledgeEntriesOutput(entries=(KnowledgeEntry(id=128, category='Example', active=True, hit_count=36),), total=1, page=1, limit=20))

setattr(list_knowledge_entries_sample_knowledge, '__module__', "dex_connectors.tiankechat.tiankechatmock")


class Mock:

    def __init__(self, connection_name: str = "tiankechat") -> None:
        self.list_knowledge_entries_script: Query[ListKnowledgeEntriesInput, ListKnowledgeEntriesOutput] = Query(LIST_KNOWLEDGE_ENTRIES_DEFINITION)
        self.list_knowledge_entries_script.default(list_knowledge_entries_sample_knowledge())
        self._connection = new_connection_with_operations(
            list_knowledge_entries=self.list_knowledge_entries_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.tiankechat.tiankechatmock")

__all__ = [
    "Mock",
    "list_knowledge_entries_listed",
    "list_knowledge_entries_provider_rejected",
    "list_knowledge_entries_invalid_response",
    "list_knowledge_entries_defect",
    "list_knowledge_entries_retry",
    "list_knowledge_entries_sample_knowledge",
]
