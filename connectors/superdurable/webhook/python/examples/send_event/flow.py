# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver one signed JSON event through a named webhook connection."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.superdurable_webhook import (
    Connection,
    SendEventInput,
    SendEventOutput,
    new_send_event_step,
)

CONNECTION_NAME = "webhook-forwarding"


# dex:group group-id:send-event group-label:"Send event"
# dex:explanation text:"Complete after the receiver acknowledges the signed event."
class CompleteDelivery(dex.Step[MutationResult[SendEventOutput]]):
    """Return the receiver status and stable webhook ID."""

    def execute(self, context: dex.Context, input: MutationResult[SendEventOutput]) -> dex.StepDecision:
        """Complete with the non-secret delivery outcome."""
        return dex.graceful_complete(input.value)


class SendEventFlow(dex.Flow[SendEventInput]):
    """Send a JSON event using the static webhook-forwarding connection."""

    def __init__(self, connection: Connection) -> None:
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("send event requires the static webhook connection")
        self.complete = CompleteDelivery()
        self.send = new_send_event_step(
            step_type="SendSignedWebhookEvent",
            step_input_type=SendEventInput,
            annotations={
                "groupId": "send-event",
                "groupLabel": "Send event",
                "explanation": "Sign and POST the event with a stable webhook ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            delivered=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the durable delivery Flow identity."""
        return "WebhookSendEventPython"

    def get_steps(self) -> dex.StepList[SendEventInput]:
        """Register the provider Mutation and completion Step."""
        return dex.StepList.start_step(self.send).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Store no Attributes or Streams for this operation-only Flow."""
        return dex.PersistenceSchema.of()
