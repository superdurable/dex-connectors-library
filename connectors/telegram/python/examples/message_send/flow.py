# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Telegram Bot Flow sending one plain text message."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.telegram import Connection, SendMessageInput, SentMessage, new_send_message_step

CONNECTION_NAME = "telegram"
_OUTCOME = dex.Attribute("telegram-python-sent-message", SentMessage)


def map_to_send_message_input(request: SendMessageInput) -> SendMessageInput:
    """Forward the non-secret chat destination and message text."""
    return request


# dex:group group-id:message-send group-label:"Message send"
# dex:explanation text:"Start one plain text send to the requested Telegram chat."
class _Start(dex.Step[SendMessageInput]):
    def get_step_type(self) -> str:
        return "StartTelegramMessageSend"

    def execute(self, context: dex.Context, input: SendMessageInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("SendTelegramMessage"), input)


# dex:group group-id:message-send group-label:"Message send"
# dex:explanation text:"Persist the confirmed Telegram message identity and complete the Flow."
class _Sent(dex.Step[MutationResult[SentMessage]]):
    def get_step_type(self) -> str:
        return "TelegramMessageSent"

    def execute(self, context: dex.Context, input: MutationResult[SentMessage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class MessageSendFlow(dex.Flow[SendMessageInput]):
    """Send one message through the static telegram connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("message send requires the static telegram connection")
        self._start = _Start()
        self._sent = _Sent()
        self._send = new_send_message_step(
            step_type="SendTelegramMessage",
            step_input_type=SendMessageInput,
            annotations={
                "groupId": "message-send",
                "groupLabel": "Message send",
                "explanation": "Send one text message behind Telegram's single-dispatch fence.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_send_message_input,
            sent=self._sent,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "TelegramPythonMessageSend"

    def get_steps(self) -> dex.StepList[SendMessageInput]:
        """Register start, provider send, and confirmation Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._send, self._sent)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the confirmed provider message identity."""
        return dex.PersistenceSchema.of(_OUTCOME)
