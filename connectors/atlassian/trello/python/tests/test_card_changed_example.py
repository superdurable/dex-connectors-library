# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic tests of the card_changed example's mapping, validation, and binding declaration."""

from dataclasses import replace
from datetime import UTC, datetime

import pytest
from dex_connectors.sdk import ConnectionRef, StaticCredentialProvider, TriggerEvent

from dex_connectors.atlassian_trello import (
    ActionCard,
    CardChangedEvent,
    Client,
    Config,
    new_connection,
)
from examples.card_changed.flow import (
    CARD_CHANGED_TRIGGER_BINDING,
    CONNECTION_NAME,
    CardChangeFlow,
    CardChangeInput,
    accepts_every_card_change,
    map_to_flow_input,
    map_to_get_card_input,
    resolve_flow_id,
    validate_card_change,
)


def _event(action_type: str = "commentCard") -> TriggerEvent[CardChangedEvent]:
    payload = CardChangedEvent(
        board_id="6512f0a1c2d3e4f5a6b70001",
        action_id="6512f0a1c2d3e4f5a6b74001",
        action_type=action_type,
        card=ActionCard(id="6512f0a1c2d3e4f5a6b71001", name="Door sensor"),
    )
    return TriggerEvent("trello-action-6512f0a1c2d3e4f5a6b74001", datetime(2026, 11, 2, tzinfo=UTC), payload)


def _flow(connection_name: str = CONNECTION_NAME) -> CardChangeFlow:
    client = Client(Config(), StaticCredentialProvider({}))
    return CardChangeFlow(new_connection(client, ConnectionRef("trello", connection_name)))


def test_flow_maps_each_card_action_to_one_flow() -> None:
    event = _event()
    assert resolve_flow_id(event) == "trello-card-change-trello-action-6512f0a1c2d3e4f5a6b74001"
    assert accepts_every_card_change(event)
    flow_input = map_to_flow_input(event)
    assert flow_input == CardChangeInput(event.id, event.payload)
    assert map_to_get_card_input(flow_input).card_id == "6512f0a1c2d3e4f5a6b71001"
    assert validate_card_change(flow_input) == ""
    assert "actionType" in validate_card_change(map_to_flow_input(_event("updateBoard")))
    assert "triggerEventId" in validate_card_change(CardChangeInput(" ", event.payload))
    no_card = replace(event.payload, card=ActionCard())
    assert "card.id" in validate_card_change(CardChangeInput(event.id, no_card))


def test_flow_declares_the_binding_and_its_required_board_input() -> None:
    flow = _flow()
    assert flow.get_flow_type() == "TrelloCardChangePython"
    [binding] = flow.get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, CARD_CHANGED_TRIGGER_BINDING)
    assert binding.configuration_ui is not None
    [unit] = binding.configuration_ui.units
    assert (unit.id, unit.unit_id, unit.label, unit.required) == (
        "watchedBoard",
        "boardInput",
        "Watched board",
        True,
    )
    assert "https://trello.com/b/<short link>/<board name>" in unit.description
    assert "leaving it blank stops the Worker" in unit.description
    assert "records a new baseline" in unit.description
    assert [(item.port, item.json_pointer) for item in unit.bindings] == [("boardId", "/boardId")]
    with pytest.raises(ValueError):
        _flow("other")
