# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListRunningAgentsInput, RunningAgent, RunningAgents
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_RUNNING_AGENTS_DEFINITION, PROVIDER_REJECTED

def list_running_agents_listed(value: RunningAgents, failure: Failure | None = None) -> Case[RunningAgents]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_running_agents_listed, '__module__', "dex_connectors.agora.agoramock")

def list_running_agents_provider_rejected(value: RunningAgents, failure: Failure | None = None) -> Case[RunningAgents]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_running_agents_provider_rejected, '__module__', "dex_connectors.agora.agoramock")

def list_running_agents_invalid_response(value: RunningAgents, failure: Failure | None = None) -> Case[RunningAgents]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_running_agents_invalid_response, '__module__', "dex_connectors.agora.agoramock")

def list_running_agents_defect(value: RunningAgents, failure: Failure | None = None) -> Case[RunningAgents]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_running_agents_defect, '__module__', "dex_connectors.agora.agoramock")

def list_running_agents_retry(failure: Failure, after_seconds: float = 0) -> Case[RunningAgents]:
    return Case.retry(failure, after_seconds)

setattr(list_running_agents_retry, '__module__', "dex_connectors.agora.agoramock")

def list_running_agents_listed_default() -> Case[RunningAgents]:
    return Case.branch_result(LISTED, RunningAgents(count=1, agents=(RunningAgent(agent_id='1234567890ABCDE1CVGZNU80BEIN56XF', status='RUNNING', start_timestamp=1735035893),), total=1, cursor=''))

setattr(list_running_agents_listed_default, '__module__', "dex_connectors.agora.agoramock")


class Mock:

    def __init__(self, connection_name: str = "agora") -> None:
        self.list_running_agents_script: Query[ListRunningAgentsInput, RunningAgents] = Query(LIST_RUNNING_AGENTS_DEFINITION)
        self.list_running_agents_script.default(list_running_agents_listed_default())
        self._connection = new_connection_with_operations(
            list_running_agents=self.list_running_agents_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.agora.agoramock")

__all__ = [
    "Mock",
    "list_running_agents_listed",
    "list_running_agents_provider_rejected",
    "list_running_agents_invalid_response",
    "list_running_agents_defect",
    "list_running_agents_retry",
    "list_running_agents_listed_default",
]
