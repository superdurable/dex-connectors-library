# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the member_changed example's Flow wiring and mappers."""

from datetime import UTC, datetime

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, StaticCredentialProvider, TriggerEvent

from dex_connectors.intuit_mailchimp import Client, Config, Member, MemberChangedEvent, new_connection
from examples.member_changed.flow import (
    BINDING_NAME,
    CONNECTION_NAME,
    MemberChangeFlow,
    MemberChangeInput,
    accepts_every_member_change,
    map_to_flow_input,
    map_to_get_member_input,
    resolve_flow_id,
    validate_member_change_input,
)


def change(email_address: str = "ben@example.com") -> TriggerEvent[MemberChangedEvent]:
    """One delivered change."""
    member = Member(
        subscriber_hash="2c66f6142933ed5a97948f89cb1c7be0", email_address=email_address, status="pending"
    )
    payload = MemberChangedEvent(audience_id="57afe96172", member=member)
    return TriggerEvent("mailchimp-0123", datetime(2026, 11, 1, tzinfo=UTC), payload)


def test_mappers_name_one_flow_per_change_and_read_the_changed_contact() -> None:
    event = change()
    assert resolve_flow_id(event) == "mailchimp-member-change-mailchimp-0123"
    flow_input = map_to_flow_input(event)
    assert flow_input == MemberChangeInput("mailchimp-0123", event.payload)
    read = map_to_get_member_input(flow_input)
    assert (read.list_id, read.email_address) == ("57afe96172", "ben@example.com")
    assert accepts_every_member_change(event)


def test_validation_refuses_an_unusable_change() -> None:
    assert validate_member_change_input(map_to_flow_input(change())) == ""
    assert "triggerEventId" in validate_member_change_input(MemberChangeInput(" ", change().payload))
    assert "emailAddress" in validate_member_change_input(map_to_flow_input(change(" ")))


async def test_flow_registers_and_declares_its_trigger_binding_units() -> None:
    async with httpx.AsyncClient() as http_client:
        client = Client(Config(), StaticCredentialProvider({}), http_client)
        flow = MemberChangeFlow(new_connection(client, ConnectionRef("mailchimp", CONNECTION_NAME)))
        dex.Registry([flow], allow_async_handlers=True)
        assert flow.get_flow_type() == "MailchimpMemberChangePython"
        [binding] = flow.get_connector_trigger_bindings()
        assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, BINDING_NAME)
        assert binding.definition.trigger.trigger_name == "memberChanged"
        assert binding.configuration_ui is not None
        units = binding.configuration_ui.to_wire()["units"]
        assert isinstance(units, list)
        assert [(unit["unitId"], unit["required"], unit["bindings"]) for unit in units] == [
            ("audienceIdEntry", True, [{"port": "audienceId", "jsonPointer": "/audienceId"}]),
            ("memberStatusPicker", False, [{"port": "statuses", "jsonPointer": "/statuses"}]),
        ]
        assert "Audience > More options > Audience settings" in units[0]["description"]
        assert "Select none" in units[1]["description"]
        with pytest.raises(ValueError, match=CONNECTION_NAME):
            MemberChangeFlow(new_connection(client, ConnectionRef("mailchimp", "other")))
