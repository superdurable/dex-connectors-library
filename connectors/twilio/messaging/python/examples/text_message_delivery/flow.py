# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Send one Twilio message and read its initial delivery status."""

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.twilio_messaging import (
    Connection,
    GetMessageInput,
    Message,
    SendMessageInput,
    new_get_message_step,
    new_send_message_step,
)

CONNECTION_NAME = "twilio-messaging"
FLOW_TYPE = "TwilioMessageDeliveryPython"


# dex:group group-id:message-delivery group-label:"Message Delivery"
# dex:explanation text:"Complete after Twilio returns the message status."
class CompleteMessageDelivery(dex.Step[QueryResult[Message]]):
    """Return the safe Twilio message status after a successful read."""

    def execute(self, context: dex.Context, input: QueryResult[Message]) -> dex.StepDecision:
        """Complete the Flow with the message resource."""
        return dex.graceful_complete(input.value)


class MessageDeliveryFlow(dex.Flow[SendMessageInput]):
    """Send one message and read it back through one static connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind both operations to the same Twilio connection."""
        if connection is None:
            raise ValueError("Twilio connection is required")
        self.complete = CompleteMessageDelivery()
        self.get = new_get_message_step(
            step_type="GetTwilioMessage",
            step_input_type=MutationResult[Message],
            annotations={
                "groupId": "message-delivery",
                "groupLabel": "Message Delivery",
                "explanation": "Read the accepted message status.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda result: GetMessageInput(result.value.sid),
            found=self.complete,
        )
        self.send = new_send_message_step(
            step_type="SendTwilioMessage",
            step_input_type=SendMessageInput,
            annotations={
                "groupId": "message-delivery",
                "groupLabel": "Message Delivery",
                "explanation": "Send one Twilio message.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            accepted=self.get,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[SendMessageInput]:
        """Run send, status read, and completion in order."""
        return dex.StepList.start_step(self.send).other_steps(self.get, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Declare the Flow's persistence schema."""
        return dex.PersistenceSchema.of()
