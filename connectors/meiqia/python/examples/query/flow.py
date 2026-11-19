# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Meiqia response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.meiqia import (
    Connection,
    GetConversationInput,
    GetConversationOutput,
    new_get_conversation_step,
)

CONNECTION_NAME = "meiqia"
_RESULT = dex.Attribute("meiqia-query-result", GetConversationOutput)


def map_to_operation_input(input: GetConversationInput) -> GetConversationInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the Meiqia provider Query."
class _StartQuery(dex.Step[GetConversationInput]):
    def get_step_type(self) -> str:
        return "StartGetConversationQuery"

    def execute(self, context: dex.Context, input: GetConversationInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetConversationQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated Meiqia Query result."
class _CompleteQuery(dex.Step[QueryResult[GetConversationOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetConversationQuery"

    def execute(self, context: dex.Context, input: QueryResult[GetConversationOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class MeiqiaGetConversationFlow(dex.Flow[GetConversationInput]):
    """Run the Meiqia Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Meiqia Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_get_conversation_step(
            step_type="GetConversationQuery",
            step_input_type=GetConversationInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Meiqia",
                "explanation": "Call Meiqia GetConversation using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "MeiqiaGetConversation"

    def get_steps(self) -> dex.StepList[GetConversationInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
