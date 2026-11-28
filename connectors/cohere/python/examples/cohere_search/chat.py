# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Generate one reply from an application-supplied conversation."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.cohere import ChatInput, ChatOutput, Connection, new_chat_step

from .flow import CONNECTION_NAME


# dex:group group-id:cohere-chat group-label:"Cohere chat"
# dex:explanation text:"Return the generated reply and its completion reason."
class _ReturnChat(dex.Step[QueryResult[ChatOutput]]):
    def get_step_type(self) -> str:
        return "ReturnCohereReply"

    def execute(self, context: dex.Context, input: QueryResult[ChatOutput]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class ChatFlow(dex.Flow[ChatInput]):
    """Run one billed chat query on the cohere connection."""

    def __init__(self, connection: Connection) -> None:
        self._complete = _ReturnChat()
        self._chat = new_chat_step(
            step_type="GenerateCohereReply",
            step_input_type=ChatInput,
            annotations={
                "groupId": "cohere-chat",
                "groupLabel": "Cohere chat",
                "explanation": "Generate a reply.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda conversation: conversation,
            answered=self._complete,
        )

    def get_flow_type(self) -> str:
        return "CohereChat"

    def get_steps(self) -> dex.StepList[ChatInput]:
        return dex.StepList.start_step(self._chat).other_steps(self._complete)
