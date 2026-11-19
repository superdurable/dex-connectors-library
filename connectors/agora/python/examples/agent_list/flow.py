# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Agora running agents through a statically named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.agora import (
    Connection,
    ListRunningAgentsInput,
    RunningAgents,
    new_list_running_agents_step,
)

CONNECTION_NAME = "agora"
_OUTCOME = dex.Attribute("agora-agent-list-outcome", RunningAgents)


def map_to_operation_input(input: ListRunningAgentsInput) -> ListRunningAgentsInput:
    """Pass the empty request to the provider Query."""
    return input


# dex:group group-id:agent-list group-label:"Agent list"
# dex:explanation text:"Start the Agora running-agent lookup."
class _StartRequest(dex.Step[ListRunningAgentsInput]):
    def get_step_type(self) -> str:
        return "StartListRunningAgents"

    def execute(self, context: dex.Context, input: ListRunningAgentsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListRunningAgents"), input)


# dex:group group-id:agent-list group-label:"Agent list"
# dex:explanation text:"Record the returned agents and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[RunningAgents]]):
    def get_step_type(self) -> str:
        return "CompleteListRunningAgents"

    def execute(self, context: dex.Context, input: QueryResult[RunningAgents]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class AgentListFlow(dex.Flow[ListRunningAgentsInput]):
    """Read one page of running agents from the `agora` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("agent list requires the static agora connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_list_running_agents_step(
            step_type="CallListRunningAgents",
            step_input_type=ListRunningAgentsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Agora",
                "explanation": "Call Agora's listRunningAgents operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable type shown in Dex Web."""
        return "AgoraAgentList"

    def get_steps(self) -> dex.StepList[ListRunningAgentsInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the latest one-page result for reads."""
        return dex.PersistenceSchema.of(_OUTCOME)
