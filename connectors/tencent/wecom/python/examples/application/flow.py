# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Send one WeCom application message through a durable Dex Flow."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.tencent_wecom import (
    Connection,
    SendApplicationMessageInput,
    SentApplicationMessage,
    new_send_application_message_step,
)

CONNECTION_NAME = "wecom-application"
_RESULT = dex.Attribute("wecom-python-application-result", SentApplicationMessage)


def map_to_message_input(input: SendApplicationMessageInput) -> SendApplicationMessageInput:
    """Pass non-secret recipients and content to the provider Step."""
    return input


# dex:group group-id:application-send group-label:"Application send"
# dex:explanation text:"Start a fenced WeCom application send."
class _Start(dex.Step[SendApplicationMessageInput]):
    def get_step_type(self) -> str:
        return "StartWeComApplicationSend"

    def execute(self, context: dex.Context, input: SendApplicationMessageInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("SendWeComApplicationMessage"), input)


# dex:group group-id:application-send group-label:"Application send"
# dex:explanation text:"Persist the confirmed WeCom application delivery."
class _Sent(dex.Step[MutationResult[SentApplicationMessage]]):
    def get_step_type(self) -> str:
        return "WeComApplicationMessageSent"

    def execute(
        self, context: dex.Context, input: MutationResult[SentApplicationMessage]
    ) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class WeComApplicationFlow(dex.Flow[SendApplicationMessageInput]):
    """Send through the static self-built application connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("WeCom application Flow requires its static connection")
        self._start = _Start()
        self._sent = _Sent()
        self._send = new_send_application_message_step(
            step_type="SendWeComApplicationMessage",
            step_input_type=SendApplicationMessageInput,
            annotations={
                "groupId": "application-send",
                "groupLabel": "Application send",
                "explanation": "Send an application message with a Fence and WeCom duplicate checking.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_message_input,
            sent=self._sent,
        )

    def get_flow_type(self) -> str:
        """Return the stable Worker Flow identity."""
        return "WeComPythonApplicationSend"

    def get_steps(self) -> dex.StepList[SendApplicationMessageInput]:
        """Register the start, provider, and confirmation Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._send, self._sent)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the confirmed secret-free result."""
        return dex.PersistenceSchema.of(_RESULT)
