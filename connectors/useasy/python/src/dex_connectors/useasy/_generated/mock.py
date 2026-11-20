# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AgentSummary, ListAgentsInput, ListAgentsOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_AGENTS_DEFINITION, PROVIDER_REJECTED

def list_agents_listed(value: ListAgentsOutput, failure: Failure | None = None) -> Case[ListAgentsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_agents_listed, '__module__', "dex_connectors.useasy.useasymock")

def list_agents_provider_rejected(value: ListAgentsOutput, failure: Failure | None = None) -> Case[ListAgentsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_agents_provider_rejected, '__module__', "dex_connectors.useasy.useasymock")

def list_agents_invalid_response(value: ListAgentsOutput, failure: Failure | None = None) -> Case[ListAgentsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_agents_invalid_response, '__module__', "dex_connectors.useasy.useasymock")

def list_agents_defect(value: ListAgentsOutput, failure: Failure | None = None) -> Case[ListAgentsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_agents_defect, '__module__', "dex_connectors.useasy.useasymock")

def list_agents_retry(failure: Failure, after_seconds: float = 0) -> Case[ListAgentsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_agents_retry, '__module__', "dex_connectors.useasy.useasymock")

def list_agents_sample_agents() -> Case[ListAgentsOutput]:
    return Case.branch_result(LISTED, ListAgentsOutput(agents=(AgentSummary(id='663578842498871236', number='8010', enabled=True),), page=1, total=1, total_pages=1))

setattr(list_agents_sample_agents, '__module__', "dex_connectors.useasy.useasymock")


class Mock:

    def __init__(self, connection_name: str = "useasy") -> None:
        self.list_agents_script: Query[ListAgentsInput, ListAgentsOutput] = Query(LIST_AGENTS_DEFINITION)
        self.list_agents_script.default(list_agents_sample_agents())
        self._connection = new_connection_with_operations(
            list_agents=self.list_agents_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.useasy.useasymock")

__all__ = [
    "Mock",
    "list_agents_listed",
    "list_agents_provider_rejected",
    "list_agents_invalid_response",
    "list_agents_defect",
    "list_agents_retry",
    "list_agents_sample_agents",
]
