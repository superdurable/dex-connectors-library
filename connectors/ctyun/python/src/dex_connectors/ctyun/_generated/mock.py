# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import KnowledgeBaseSummary, ListKnowledgeBasesInput, ListKnowledgeBasesOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_KNOWLEDGE_BASES_DEFINITION, PROVIDER_REJECTED

def list_knowledge_bases_listed(value: ListKnowledgeBasesOutput, failure: Failure | None = None) -> Case[ListKnowledgeBasesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_knowledge_bases_listed, '__module__', "dex_connectors.ctyun.ctyunmock")

def list_knowledge_bases_provider_rejected(value: ListKnowledgeBasesOutput, failure: Failure | None = None) -> Case[ListKnowledgeBasesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_knowledge_bases_provider_rejected, '__module__', "dex_connectors.ctyun.ctyunmock")

def list_knowledge_bases_invalid_response(value: ListKnowledgeBasesOutput, failure: Failure | None = None) -> Case[ListKnowledgeBasesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_knowledge_bases_invalid_response, '__module__', "dex_connectors.ctyun.ctyunmock")

def list_knowledge_bases_defect(value: ListKnowledgeBasesOutput, failure: Failure | None = None) -> Case[ListKnowledgeBasesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_knowledge_bases_defect, '__module__', "dex_connectors.ctyun.ctyunmock")

def list_knowledge_bases_retry(failure: Failure, after_seconds: float = 0) -> Case[ListKnowledgeBasesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_knowledge_bases_retry, '__module__', "dex_connectors.ctyun.ctyunmock")

def list_knowledge_bases_sample_knowledge_bases() -> Case[ListKnowledgeBasesOutput]:
    return Case.branch_result(LISTED, ListKnowledgeBasesOutput(knowledge_bases=(KnowledgeBaseSummary(id=280, name='Example knowledge base', file_count=4),)))

setattr(list_knowledge_bases_sample_knowledge_bases, '__module__', "dex_connectors.ctyun.ctyunmock")


class Mock:

    def __init__(self, connection_name: str = "ctyun") -> None:
        self.list_knowledge_bases_script: Query[ListKnowledgeBasesInput, ListKnowledgeBasesOutput] = Query(LIST_KNOWLEDGE_BASES_DEFINITION)
        self.list_knowledge_bases_script.default(list_knowledge_bases_sample_knowledge_bases())
        self._connection = new_connection_with_operations(
            list_knowledge_bases=self.list_knowledge_bases_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.ctyun.ctyunmock")

__all__ = [
    "Mock",
    "list_knowledge_bases_listed",
    "list_knowledge_bases_provider_rejected",
    "list_knowledge_bases_invalid_response",
    "list_knowledge_bases_defect",
    "list_knowledge_bases_retry",
    "list_knowledge_bases_sample_knowledge_bases",
]
