# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Persist nonsecret metadata for one verified Typeform form response."""

from dataclasses import dataclass, field
from datetime import datetime

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.typeform import FormResponseEvent, define_response_submitted_trigger_binding

CONNECTION_NAME = "typeform-forms"
BINDING_NAME = "response-submitted"


@dataclass(frozen=True, slots=True)
class Submission:
    """Nonsecret Flow input for a verified response."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    form_id: str = field(metadata={"dex_json_name": "formId"})
    response_token: str = field(metadata={"dex_json_name": "responseToken"})
    submitted_at: datetime = field(metadata={"dex_json_name": "submittedAt", "dex_json_format": "rfc3339"})


submission_attribute = dex.Attribute("typeform-response-submission", Submission)


def accept_response(event: TriggerEvent[FormResponseEvent]) -> bool:
    """Accept each decoded form_response; the binding applies the selected form filter."""
    return bool(event.payload.response.token)


def resolve_flow_id(event: TriggerEvent[FormResponseEvent]) -> str:
    """Reuse the same Flow ID when Typeform redelivers the response."""
    return "typeform-" + event.id.replace(":", "-")


def map_to_flow_input(event: TriggerEvent[FormResponseEvent]) -> Submission:
    """Keep answers and hidden fields out of the Flow input."""
    return Submission(event.id, event.payload.form_id, event.payload.response.token, event.occurred_at)


# dex:group group-id:typeform group-label:"Typeform"
# dex:explanation text:"Persist the verified response identity and completion time."
class RecordSubmission(dex.Step[Submission]):
    """Complete a Flow with the accepted response identity."""

    def get_step_type(self) -> str:
        """Return the stable recording Step identity."""
        return "RecordSubmission"

    def execute(self, context: dex.Context, input: Submission) -> dex.StepDecision:
        """Persist and complete one verified submission."""
        submission_attribute.set(context, input)
        return dex.graceful_complete(input)


class ResponseIntakeFlow(dex.Flow[Submission]):
    """Record each signed Typeform response in its own durable Flow."""

    def __init__(self) -> None:
        """Construct a static Trigger-driven Flow."""
        self.record = RecordSubmission()

    def get_flow_type(self) -> str:
        """Return the Dex Flow type."""
        return "TypeformResponseIntakePython"

    def get_steps(self) -> dex.StepList[Submission]:
        """Register the single recording Step."""
        return dex.StepList.start_step(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the persisted submission Attribute."""
        return dex.PersistenceSchema.of(submission_attribute)

    # Expose Typeform's form picker for this Trigger binding in Dex Web.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_response_submitted_trigger_binding(
                connection_name="typeform-forms",
                binding_name="response-submitted",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "form",
                            "formPicker",
                            "Form",
                            "Select the Typeform form whose completed responses start this Flow. "
                            "The picker lists the connected account's forms and stores the form ID. "
                            "Leave blank to accept every form whose webhook points here.",
                            False,
                            (ConnectorUIBinding("formId", "/formId"),),
                        ),
                    )
                ),
            )
        ]
