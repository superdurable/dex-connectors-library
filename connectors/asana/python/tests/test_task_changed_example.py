# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the taskChanged example's Flow wiring and mapping."""

from datetime import UTC, datetime

import dex
import httpx
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerEvent

from dex_connectors.asana import (
    Client,
    Config,
    Credentials,
    TaskChangedEvent,
    TaskReference,
    new_connection,
)
from examples.task_changed.flow import (
    CONNECTION_NAME,
    TASK_CHANGED_TRIGGER_BINDING,
    TaskChangedFlow,
    TaskChangeInput,
    map_to_flow_input,
    map_to_get_task_input,
    resolve_flow_id,
    validate_task_change_input,
)

CHANGE = TaskChangedEvent(
    project_id="1201000000000001",
    action="changed",
    task=TaskReference(id="1204000000000001", name="Inspect door sensors"),
    changed_field="completed",
    change_action="changed",
    occurred_at="2026-03-02T09:00:01Z",
)
EVENT = TriggerEvent(
    "asana-94ef95efc2e9cbae0b7708d10be4af34", datetime(2026, 3, 2, 9, 0, 1, tzinfo=UTC), CHANGE
)


def new_flow() -> TaskChangedFlow:
    reference = ConnectionRef("asana", CONNECTION_NAME)
    credentials = StaticCredentialProvider({reference: Credentials(SecretString("asana-fake-token"))})
    return TaskChangedFlow(new_connection(Client(Config(), credentials, httpx.AsyncClient()), reference))


def test_each_event_names_one_flow_and_reads_its_task() -> None:
    assert resolve_flow_id(EVENT) == "asana-task-change-asana-94ef95efc2e9cbae0b7708d10be4af34"
    flow_input = map_to_flow_input(EVENT)
    assert flow_input == TaskChangeInput(trigger_event_id=EVENT.id, change=CHANGE)
    assert map_to_get_task_input(flow_input).task_id == "1204000000000001"
    assert validate_task_change_input(flow_input) == ""


def test_invalid_deliveries_are_refused() -> None:
    assert validate_task_change_input(TaskChangeInput(trigger_event_id=" ", change=CHANGE))
    unknown = TaskChangedEvent(project_id="1", action="moved", task=TaskReference(id="2"))
    assert "added, changed" in validate_task_change_input(TaskChangeInput("event", unknown))
    missing = TaskChangedEvent(project_id="1", action="added")
    assert "task.id" in validate_task_change_input(TaskChangeInput("event", missing))


def test_flow_registers_and_declares_the_task_changed_binding_with_its_project_picker() -> None:
    flow = new_flow()
    dex.Registry([flow], allow_async_handlers=True)
    (binding,) = flow.get_connector_trigger_bindings()
    assert binding.binding_name == TASK_CHANGED_TRIGGER_BINDING
    assert binding.connection_name == CONNECTION_NAME
    assert binding.definition.trigger.trigger_name == "taskChanged"
    assert binding.configuration_ui is not None
    (unit,) = binding.configuration_ui.units
    assert (unit.unit_id, unit.required) == ("projectPicker", True)
    assert [(item.port, item.json_pointer) for item in unit.bindings] == [
        ("workspaceId", "/workspaceId"),
        ("projectId", "/projectId"),
        ("projectName", "/projectName"),
    ]
    assert "/project/" in unit.description and "baseline" in unit.description
