# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable iFLYTEK Spark chat completion Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.iflytek import (
    ChatCompletion,
    Connection,
    CreateChatCompletionInput,
    new_create_chat_completion_step,
)

CONNECTION_NAME = "iflytek"
_RESULT = dex.Attribute("iflytek-chat-completion", ChatCompletion)


def map_to_operation_input(request: CreateChatCompletionInput) -> CreateChatCompletionInput:
    """Forward the bounded chat prompt."""
    return request


# dex:group group-id:iflytek group-label:"iFLYTEK Spark"
# dex:explanation text:"Start one bounded Spark chat completion."
class _StartChatCompletion(dex.Step[CreateChatCompletionInput]):
    def get_step_type(self) -> str:
        return "StartIflytekChatCompletion"

    def execute(self, context: dex.Context, input: CreateChatCompletionInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallIflytek"), input)


# dex:group group-id:iflytek group-label:"iFLYTEK Spark"
# dex:explanation text:"Persist assistant text and token counts before completion."
class _CompleteChatCompletion(dex.Step[QueryResult[ChatCompletion]]):
    def get_step_type(self) -> str:
        return "CompleteIflytekChatCompletion"

    def execute(self, context: dex.Context, input: QueryResult[ChatCompletion]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ChatCompletionFlow(dex.Flow[CreateChatCompletionInput]):
    """Generate one answer through the static iFLYTEK connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ChatCompletionFlow requires the static iFLYTEK connection")
        self._start = _StartChatCompletion()
        self._complete = _CompleteChatCompletion()
        self._call = new_create_chat_completion_step(
            step_type="CallIflytek",
            step_input_type=CreateChatCompletionInput,
            annotations={
                "groupId": "iflytek",
                "groupLabel": "iFLYTEK Spark",
                "explanation": "Generate one bounded Spark answer.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            completed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable chat completion Flow identity."""
        return "IflytekChatCompletion"

    def get_steps(self) -> dex.StepList[CreateChatCompletionInput]:
        """Register the chat completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the answer and token counts only."""
        return dex.PersistenceSchema.of(_RESULT)
