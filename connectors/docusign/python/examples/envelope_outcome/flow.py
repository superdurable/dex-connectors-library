# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Flow per DocuSign Connect envelope outcome, read the envelope back, and record the outcome."""

import re
from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult, TriggerBindingDefinition, TriggerEvent

from dex_connectors.docusign import (
    CONNECT_ENVELOPE_EVENTS,
    Connection,
    Envelope,
    EnvelopeEvent,
    GetEnvelopeInput,
    define_envelope_event_received_trigger_binding,
    envelope_event_custom_field_value,
    new_get_envelope_step,
)

FLOW_TYPE = "DocuSignEnvelopeOutcomePython"
CONNECTION_NAME = "docusign-esignature"
ENVELOPE_OUTCOME_TRIGGER_BINDING = "envelope-outcomes"
FLOW_ID_PREFIX = "docusign-envelope-outcome-"
CORRELATION_CUSTOM_FIELD_NAME = "dexSigningRequestId"
"""Hidden envelope custom field that carries the application's signing request ID back through Connect."""
READ_ENVELOPE_STEP_TYPE = "ReadEnvelope"
_REQUEST_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9_-]{0,63}\Z")


@dataclass(frozen=True, slots=True)
class EnvelopeOutcomeInput:
    """One envelopeEventReceived delivery with the Trigger event ID that also names the Flow."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    received: EnvelopeEvent = field(metadata={"dex_json_name": "received"})


@dataclass(frozen=True, slots=True)
class EnvelopeOutcome:
    """The Flow result: the Connect outcome, its signing request, and the envelope read back from DocuSign."""

    event: str = field(metadata={"dex_json_name": "event"})
    envelope_id: str = field(metadata={"dex_json_name": "envelopeId"})
    signing_request_id: str = field(metadata={"dex_json_name": "signingRequestId"})
    current: Envelope = field(metadata={"dex_json_name": "current"})


envelope_event_attribute = dex.Attribute("docusign-envelope-event", EnvelopeOutcomeInput)
envelope_outcome_attribute = dex.Attribute("docusign-envelope-outcome", EnvelopeOutcome)


def resolve_flow_id(event: TriggerEvent[EnvelopeEvent]) -> str:
    """Name one Flow per envelope outcome; a Connect retry or second configuration finds the same Flow."""
    return FLOW_ID_PREFIX + event.id.replace(":", "-")


def map_to_flow_input(event: TriggerEvent[EnvelopeEvent]) -> EnvelopeOutcomeInput:
    """Keep the Trigger event ID beside the delivered envelope event."""
    return EnvelopeOutcomeInput(event.id, event.payload)


def accept_correlated_envelope_event(event: TriggerEvent[EnvelopeEvent]) -> bool:
    """Admit only an envelope this application sent, which carries a well-formed signing request ID."""
    request_id = envelope_event_custom_field_value(event.payload, CORRELATION_CUSTOM_FIELD_NAME)
    return request_id is not None and _REQUEST_ID.match(request_id) is not None


def map_to_get_envelope_input(input: EnvelopeOutcomeInput) -> GetEnvelopeInput:
    """Read the envelope by its ID."""
    return GetEnvelopeInput(input.received.envelope_id)


def _invalid_input_reason(input: EnvelopeOutcomeInput) -> str:
    if not input.trigger_event_id.strip() or not input.received.envelope_id.strip():
        return "triggerEventId and received.envelopeId are required"
    if input.received.event not in CONNECT_ENVELOPE_EVENTS:
        return "received.event must be envelope-completed, envelope-declined, or envelope-voided"
    return ""


# dex:group group-id:outcome group-label:"Envelope outcome"
# dex:explanation text:"Record the delivered Connect envelope event, then read the envelope back."
class RecordEnvelopeEvent(dex.Step[EnvelopeOutcomeInput]):
    """Validate and persist the delivered event, then read the envelope back."""

    def __init__(self, read_envelope: dex.StepTarget[EnvelopeOutcomeInput]) -> None:
        """Bind the connector Step that reads the envelope back."""
        self._read_envelope = read_envelope

    def get_step_type(self) -> str:
        """Keep the Step identity stable."""
        return "RecordEnvelopeEvent"

    def execute(self, context: dex.Context, input: EnvelopeOutcomeInput) -> dex.StepDecision:
        """Fail an incomplete delivery; otherwise record it and read the envelope back."""
        reason = _invalid_input_reason(input)
        if reason:
            return dex.force_fail(reason)
        envelope_event_attribute.set(context, input)
        return dex.go_to(self._read_envelope, input)


# dex:group group-id:outcome group-label:"Envelope outcome"
# dex:explanation text:"Record the outcome with the envelope read back from DocuSign and complete the Flow."
class CompleteEnvelopeOutcome(dex.Step[QueryResult[Envelope]]):
    """Record the Connect outcome and the current envelope."""

    def get_step_type(self) -> str:
        """Keep the Step identity stable."""
        return "CompleteEnvelopeOutcome"

    def execute(self, context: dex.Context, input: QueryResult[Envelope]) -> dex.StepDecision:
        """Complete the Flow with the delivered outcome and the envelope read back."""
        received = envelope_event_attribute.get(context).received
        request_id = envelope_event_custom_field_value(received, CORRELATION_CUSTOM_FIELD_NAME) or ""
        outcome = EnvelopeOutcome(received.event, received.envelope_id, request_id, input.value)
        envelope_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class EnvelopeOutcomeFlow(dex.Flow[EnvelopeOutcomeInput]):
    """Record one envelope outcome started by the envelopeEventReceived Trigger."""

    def __init__(self, connection: Connection) -> None:
        """Bind the DocuSign connection before Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("envelope outcome requires the static DocuSign connection")
        self.complete = CompleteEnvelopeOutcome()
        self.read_envelope = new_get_envelope_step(
            step_type=READ_ENVELOPE_STEP_TYPE,
            step_input_type=EnvelopeOutcomeInput,
            annotations={
                "groupId": "outcome",
                "groupLabel": "Envelope outcome",
                "explanation": "Read the envelope back from DocuSign for its current status.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_envelope_input,
            found=self.complete,
        )
        self.record = RecordEnvelopeEvent(self.read_envelope)

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "DocuSignEnvelopeOutcomePython"

    def get_steps(self) -> dex.StepList[EnvelopeOutcomeInput]:
        """Record the event, read the envelope back, and complete."""
        return dex.StepList.start_step(self.record).other_steps(self.read_envelope, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the delivery and outcome Attributes."""
        return dex.PersistenceSchema.of(envelope_event_attribute, envelope_outcome_attribute)

    # The binding has no configuration units, so Dex Web saves {}, which records all three outcomes.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_envelope_event_received_trigger_binding(
                connection_name="docusign-esignature", binding_name="envelope-outcomes"
            )
        ]
