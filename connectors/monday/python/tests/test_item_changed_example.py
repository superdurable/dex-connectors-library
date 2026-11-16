# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the item_changed example's Flow wiring, binding, and mappers."""

from datetime import UTC, datetime

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, StaticCredentialProvider, TriggerEvent

from dex_connectors.monday import Client, Config, Item, ItemChangedEvent, new_connection
from examples.item_changed.flow import (
    CONNECTION_NAME,
    FLOW_TYPE,
    ITEM_CHANGED_TRIGGER_BINDING,
    ItemChangeFlow,
    ItemChangeInput,
    accept_every_item_change,
    map_to_flow_input,
    map_to_get_item_input,
    resolve_flow_id,
    validate_item_change_input,
)


def change(kind: str = "updated", item_id: str = "5550001") -> TriggerEvent[ItemChangedEvent]:
    """One delivered change."""
    payload = ItemChangedEvent("1234567890", change_kind=kind, item=Item(item_id, "Request"))
    return TriggerEvent("monday-item-0123", datetime(2026, 3, 2, 9, tzinfo=UTC), payload)


def test_mappers_name_one_flow_per_change_and_read_the_changed_item() -> None:
    event = change()
    assert resolve_flow_id(event) == "monday-item-change-monday-item-0123"
    flow_input = map_to_flow_input(event)
    assert flow_input == ItemChangeInput("monday-item-0123", event.payload)
    assert map_to_get_item_input(flow_input).item_id == "5550001"
    assert accept_every_item_change(change("created"))


def test_validation_refuses_an_unusable_change() -> None:
    assert validate_item_change_input(map_to_flow_input(change())) == ""
    assert "triggerEventId" in validate_item_change_input(ItemChangeInput(" ", change().payload))
    assert "item.id" in validate_item_change_input(map_to_flow_input(change(item_id="")))
    assert "changeKind" in validate_item_change_input(map_to_flow_input(change(kind="archived")))


async def test_flow_registers_and_declares_its_trigger_binding_and_board_field() -> None:
    async with httpx.AsyncClient() as http_client:
        client = Client(Config(), StaticCredentialProvider({}), http_client)
        flow = ItemChangeFlow(new_connection(client, ConnectionRef("monday", CONNECTION_NAME)))
        dex.Registry([flow], allow_async_handlers=True)
        assert flow.get_flow_type() == FLOW_TYPE
        [binding] = flow.get_connector_trigger_bindings()
        assert (binding.connection_name, binding.binding_name) == (
            CONNECTION_NAME,
            ITEM_CHANGED_TRIGGER_BINDING,
        )
        assert binding.definition.trigger.trigger_name == "itemChanged"
        assert binding.configuration_ui is not None
        [unit] = binding.configuration_ui.units
        assert (unit.unit_id, unit.required) == ("boardIdInput", True)
        assert [(port.port, port.json_pointer) for port in unit.bindings] == [("boardId", "/boardId")]
        assert "https://monday.com/" in unit.description and "/boards/" in unit.description
        assert "not secret" in unit.description and "Blank saves nothing" in unit.description
        with pytest.raises(ValueError, match=CONNECTION_NAME):
            ItemChangeFlow(new_connection(client, ConnectionRef("monday", "other")))
