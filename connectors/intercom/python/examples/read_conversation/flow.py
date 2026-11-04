# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Durable read of one Intercom conversation and its latest parts."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.intercom import (
    Connection,
    ConversationDetails,
    GetConversationInput,
    new_get_conversation_step,
)

CONNECTION_NAME = "intercom-support-inbox"
FLOW_TYPE = "IntercomReadConversationPython"
_DETAILS = dex.Attribute("intercom-read-conversation-details", ConversationDetails)


# dex:group group-id:intercom-read group-label:"Intercom conversation"
# dex:explanation text:"Save the retrieved Intercom conversation and complete the Flow."
class ConversationFound(dex.Step[QueryResult[ConversationDetails]]):
    """Persist the result for Dex Web and finish the Flow."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "ConversationFound"

    def execute(self, context: dex.Context, input: QueryResult[ConversationDetails]) -> dex.StepDecision:
        """Save the selected conversation and complete."""
        _DETAILS.set(context, input.value)
        return dex.graceful_complete(input.value)


class ReadConversationFlow(dex.Flow[GetConversationInput]):
    """Read a conversation through the static Intercom connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind a named Intercom connection for Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("read conversation requires the static Intercom connection")
        self.found = ConversationFound()
        # dex:group group-id:intercom-read group-label:"Intercom conversation"
        # dex:explanation text:"Read one conversation and its newest parts from Intercom."
        self.read = new_get_conversation_step(
            step_type="GetConversation",
            step_input_type=GetConversationInput,
            annotations={
                "groupId": "intercom-read",
                "groupLabel": "Intercom conversation",
                "explanation": "Read one conversation and its newest parts from Intercom.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: request,
            found=self.found,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetConversationInput]:
        """Register the provider read and completion Steps."""
        return dex.StepList.start_step(self.read).other_steps(self.found)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the completed conversation Attribute."""
        return dex.PersistenceSchema.of(_DETAILS)
