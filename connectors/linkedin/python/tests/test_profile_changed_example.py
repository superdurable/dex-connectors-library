# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the profile_changed example's Flow wiring and mappers."""

from datetime import UTC, datetime

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, StaticCredentialProvider, TriggerEvent

from dex_connectors.linkedin import Client, Config, ProfileChangedEvent, ProfileClaims, new_connection
from examples.profile_changed.flow import (
    BINDING_NAME,
    CONNECTION_NAME,
    ProfileChangeFlow,
    ProfileChangeInput,
    accepts_every_profile_change,
    map_to_flow_input,
    map_to_get_authenticated_profile_input,
    resolve_flow_id,
    validate_profile_change_input,
)


def change(
    subject: str = "782bbtaQ", fields: tuple[str, ...] = ("name",)
) -> TriggerEvent[ProfileChangedEvent]:
    """One delivered change."""
    payload = ProfileChangedEvent(
        subject=subject,
        previous=ProfileClaims(subject=subject, name="Ada Lovelace"),
        current=ProfileClaims(subject=subject, name="Ada King"),
        changed_fields=fields,
    )
    return TriggerEvent("linkedin-profile-0123", datetime(2036, 2, 23, tzinfo=UTC), payload)


def test_mappers_name_one_flow_per_change_and_read_the_verified_profile() -> None:
    event = change()
    assert resolve_flow_id(event) == "linkedin-profile-change-linkedin-profile-0123"
    flow_input = map_to_flow_input(event)
    assert flow_input == ProfileChangeInput("linkedin-profile-0123", event.payload)
    assert map_to_get_authenticated_profile_input(flow_input) is not None
    assert accepts_every_profile_change(event)


def test_validation_refuses_an_unusable_change() -> None:
    assert validate_profile_change_input(map_to_flow_input(change())) == ""
    assert "triggerEventId" in validate_profile_change_input(ProfileChangeInput(" ", change().payload))
    assert "subject" in validate_profile_change_input(map_to_flow_input(change(subject=" ")))
    assert "changedFields" in validate_profile_change_input(map_to_flow_input(change(fields=())))
    assert "unknown" in validate_profile_change_input(map_to_flow_input(change(fields=("headline",))))


async def test_flow_registers_and_declares_its_trigger_binding() -> None:
    async with httpx.AsyncClient() as http_client:
        client = Client(Config(), StaticCredentialProvider({}), http_client)
        flow = ProfileChangeFlow(new_connection(client, ConnectionRef("linkedin", CONNECTION_NAME)))
        dex.Registry([flow], allow_async_handlers=True)
        assert flow.get_flow_type() == "LinkedInProfileChangePython"
        [binding] = flow.get_connector_trigger_bindings()
        assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, BINDING_NAME)
        assert binding.definition.trigger.trigger_name == "profileChanged"
        assert binding.configuration_ui is None, "the binding watches the connected member and has no field"
        with pytest.raises(ValueError, match=CONNECTION_NAME):
            ProfileChangeFlow(new_connection(client, ConnectionRef("linkedin", "other")))
