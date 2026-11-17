# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Record each verified form submission in one Dex Flow."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import TriggerBindingDefinition, TriggerEvent

from dex_connectors.superdurable_webhook import (
    Connection,
    WebhookRequestEvent,
    define_request_received_trigger_binding,
)

CONNECTION_NAME = "webhook-form"
BINDING_NAME = "submissions"
FLOW_ID_PREFIX = "webhook-form-submission-"


@dataclass(frozen=True, slots=True)
class SubmissionInput:
    """One verified request and its stable sender or body-derived ID."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    received: WebhookRequestEvent


submission_attribute = dex.Attribute("webhook-form-submission-python", SubmissionInput)


def resolve_flow_id(event: TriggerEvent[WebhookRequestEvent]) -> str:
    """Use the verified event ID to deduplicate redelivery."""
    return FLOW_ID_PREFIX + event.id


def map_to_flow_input(event: TriggerEvent[WebhookRequestEvent]) -> SubmissionInput:
    """Carry the verified request into the Flow."""
    return SubmissionInput(event.id, event.payload)


# dex:group group-id:form-submission group-label:"Form submission"
# dex:explanation text:"Persist the verified request and complete the Flow."
class RecordSubmission(dex.Step[SubmissionInput]):
    """Persist one form submission in a Dex Attribute."""

    def get_step_type(self) -> str:
        """Keep this Step identity stable."""
        return "RecordWebhookFormSubmission"

    def execute(self, context: dex.Context, input: SubmissionInput) -> dex.StepDecision:
        """Record the request and complete the Flow."""
        if not input.event_id or input.received.content_type not in {
            "application/json",
            "application/x-www-form-urlencoded",
        }:
            return dex.force_fail("verified event ID and supported content type are required")
        submission_attribute.set(context, input)
        return dex.graceful_complete(input)


class SubmissionFlow(dex.Flow[SubmissionInput]):
    """Own one verified form submission per stable event ID."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static webhook connection."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("submission Flow requires the static webhook connection")
        self.record = RecordSubmission()

    def get_flow_type(self) -> str:
        """Return the durable Flow identity."""
        return "WebhookFormSubmissionPython"

    def get_steps(self) -> dex.StepList[SubmissionInput]:
        """Start at the recording Step."""
        return dex.StepList.start_step(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the submission Attribute."""
        return dex.PersistenceSchema.of(submission_attribute)

    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_request_received_trigger_binding(
                connection_name="webhook-form", binding_name="submissions"
            )
        ]
