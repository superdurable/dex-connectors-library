# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check the envelope_outcome example's correlation, Flow IDs, input mapping, and registration."""

from datetime import UTC, datetime

import dex
from dex_connectors.sdk import TriggerEvent

from dex_connectors.docusign import EnvelopeCustomField, EnvelopeEvent
from dex_connectors.docusign.docusignmock import Mock
from examples.envelope_outcome.flow import (
    CONNECTION_NAME,
    ENVELOPE_OUTCOME_TRIGGER_BINDING,
    EnvelopeOutcomeFlow,
    EnvelopeOutcomeInput,
    accept_correlated_envelope_event,
    map_to_flow_input,
    map_to_get_envelope_input,
    resolve_flow_id,
)
from tests.connect_fixture import TEST_ACCOUNT_ID, TEST_ENVELOPE_ID


def outcome_event(*custom_fields: EnvelopeCustomField) -> TriggerEvent[EnvelopeEvent]:
    generated_at = datetime(2026, 10, 4, 13, tzinfo=UTC)
    payload = EnvelopeEvent(
        "envelope-completed", TEST_ENVELOPE_ID, TEST_ACCOUNT_ID, generated_at, "completed", "", custom_fields
    )
    return TriggerEvent(TEST_ENVELOPE_ID + ":envelope-completed", generated_at, payload)


def test_admits_only_envelopes_with_a_well_formed_signing_request_id() -> None:
    assert accept_correlated_envelope_event(
        outcome_event(EnvelopeCustomField("dexSigningRequestId", "opp-123"))
    )
    assert not accept_correlated_envelope_event(outcome_event())
    for value in ("", "-opp", "opp 123", "x" * 65):
        assert not accept_correlated_envelope_event(
            outcome_event(EnvelopeCustomField("dexSigningRequestId", value))
        )


def test_names_one_flow_per_outcome_and_maps_its_input() -> None:
    event = outcome_event(EnvelopeCustomField("dexSigningRequestId", "opp-123"))
    assert resolve_flow_id(event) == "docusign-envelope-outcome-" + TEST_ENVELOPE_ID + "-envelope-completed"
    flow_input = map_to_flow_input(event)
    assert flow_input == EnvelopeOutcomeInput(event.id, event.payload)
    assert map_to_get_envelope_input(flow_input).envelope_id == TEST_ENVELOPE_ID


def test_registers_the_flow_and_its_static_binding() -> None:
    flow = EnvelopeOutcomeFlow(Mock(CONNECTION_NAME).connection())
    dex.Registry([flow], allow_async_handlers=True)
    [binding] = flow.get_connector_trigger_bindings()
    assert binding.connection_name == CONNECTION_NAME
    assert binding.binding_name == ENVELOPE_OUTCOME_TRIGGER_BINDING
    assert binding.definition.trigger.trigger_name == "envelopeEventReceived"
