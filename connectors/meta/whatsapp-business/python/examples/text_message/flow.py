# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable WhatsApp Business text publishing Flow."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.meta_whatsapp_business import (
    Connection,
    SendTextMessageInput,
    SentMessage,
    new_send_text_message_step,
)

FLOW_TYPE = "WhatsAppBusinessTextMessagePython"
CONNECTION_NAME = "whatsapp-business"
_CALL_STEP_TYPE = "CallSendTextMessage"
_RESULT = dex.Attribute("whatsapp-business-python-text-message", SentMessage)


def map_to_operation_input(request: SendTextMessageInput) -> SendTextMessageInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:whatsapp-business group-label:"WhatsApp Business"
# dex:explanation text:"Start the WhatsApp Business request."
class _StartRequest(dex.Step[SendTextMessageInput]):
    def get_step_type(self) -> str:
        return "StartSendTextMessage"

    def execute(self, context: dex.Context, input: SendTextMessageInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallSendTextMessage"), input)


# dex:group group-id:whatsapp-business group-label:"WhatsApp Business"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[MutationResult[SentMessage]]):
    def get_step_type(self) -> str:
        return "CompleteSendTextMessage"

    def execute(self, context: dex.Context, input: MutationResult[SentMessage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TextMessageFlow(dex.Flow[SendTextMessageInput]):
    """Send one text message through the static whatsapp-business connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the WhatsApp messaging Flow requires the static whatsapp-business connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_send_text_message_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=SendTextMessageInput,
            annotations={
                "groupId": "whatsapp-business",
                "groupLabel": "WhatsApp Business",
                "explanation": "Send one text message using the system-user token.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            sent=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "WhatsAppBusinessTextMessagePython"

    def get_steps(self) -> dex.StepList[SendTextMessageInput]:
        """Register the start, provider send, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
