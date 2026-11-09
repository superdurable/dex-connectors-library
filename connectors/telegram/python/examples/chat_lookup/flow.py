# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Telegram Bot Flow looking up one accessible chat."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.telegram import Chat, Connection, GetChatInput, new_get_chat_step

CONNECTION_NAME = "telegram"
_OUTCOME = dex.Attribute("telegram-python-chat", Chat)


def map_to_get_chat_input(request: GetChatInput) -> GetChatInput:
    """Forward the non-secret chat selector."""
    return request


# dex:group group-id:chat-lookup group-label:"Chat lookup"
# dex:explanation text:"Start a metadata lookup for one bot-accessible Telegram chat."
class _Start(dex.Step[GetChatInput]):
    def get_step_type(self) -> str:
        return "StartTelegramChatLookup"

    def execute(self, context: dex.Context, input: GetChatInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetTelegramChat"), input)


# dex:group group-id:chat-lookup group-label:"Chat lookup"
# dex:explanation text:"Persist the returned chat identity and complete the Flow."
class _Found(dex.Step[QueryResult[Chat]]):
    def get_step_type(self) -> str:
        return "TelegramChatFound"

    def execute(self, context: dex.Context, input: QueryResult[Chat]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ChatLookupFlow(dex.Flow[GetChatInput]):
    """Read one chat through the static telegram connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("chat lookup requires the static telegram connection")
        self._start = _Start()
        self._found = _Found()
        self._get = new_get_chat_step(
            step_type="GetTelegramChat",
            step_input_type=GetChatInput,
            annotations={
                "groupId": "chat-lookup",
                "groupLabel": "Chat lookup",
                "explanation": "Read the accessible chat's basic Telegram metadata.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_chat_input,
            found=self._found,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "TelegramPythonChatLookup"

    def get_steps(self) -> dex.StepList[GetChatInput]:
        """Register start, provider lookup, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._get, self._found)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only basic chat metadata."""
        return dex.PersistenceSchema.of(_OUTCOME)
