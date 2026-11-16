# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the employee_changed example's Flow wiring and mappers."""

from dataclasses import replace
from datetime import UTC, datetime

import dex
import pytest
from dex_connectors.sdk import TriggerEvent

from dex_connectors.bamboohr import EmployeeChangedEvent
from dex_connectors.bamboohr.bamboohrmock import Mock
from examples.employee_changed.flow import (
    BINDING_NAME,
    CONNECTION_NAME,
    HAND_OFF_FIELDS,
    EmployeeChangeFlow,
    EmployeeChangeInput,
    accepts_every_employee_change,
    map_to_flow_input,
    map_to_get_employee_input,
    resolve_flow_id,
    validate_employee_change_input,
)


def change(action: str = "Updated", employee_id: str = "140") -> TriggerEvent[EmployeeChangedEvent]:
    """One delivered change."""
    last_changed = datetime(2026, 10, 13, 9, 0, 1, tzinfo=UTC)
    payload = EmployeeChangedEvent(employee_id, action=action, last_changed=last_changed)
    return TriggerEvent(f"employee-{employee_id}-{action.lower()}-1791882001", last_changed, payload)


def test_mappers_name_one_flow_per_change_and_read_the_hand_off_fields() -> None:
    event = change()
    assert resolve_flow_id(event) == "bamboohr-employee-change-employee-140-updated-1791882001"
    flow_input = map_to_flow_input(event)
    assert flow_input == EmployeeChangeInput("employee-140-updated-1791882001", event.payload)
    read = map_to_get_employee_input(flow_input)
    assert (read.employee_id, read.fields) == ("140", HAND_OFF_FIELDS)
    assert accepts_every_employee_change(change("Deleted"))


def test_validation_refuses_an_unusable_change() -> None:
    assert validate_employee_change_input(map_to_flow_input(change())) == ""
    assert "triggerEventId" in validate_employee_change_input(EmployeeChangeInput(" ", change().payload))
    assert "employeeId" in validate_employee_change_input(map_to_flow_input(change(employee_id="")))
    unset = replace(change().payload, last_changed=datetime.min.replace(tzinfo=UTC))
    assert "lastChanged" in validate_employee_change_input(EmployeeChangeInput("id", unset))
    assert "action" in validate_employee_change_input(map_to_flow_input(change("Terminated")))


def test_flow_registers_and_declares_its_trigger_binding() -> None:
    flow = EmployeeChangeFlow(Mock(CONNECTION_NAME).connection())
    dex.Registry([flow], allow_async_handlers=True)
    assert flow.get_flow_type() == "BambooHREmployeeChangePython"
    [binding] = flow.get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, BINDING_NAME)
    assert binding.definition.trigger.trigger_name == "employeeChanged"
    assert binding.configuration_ui is not None
    [unit] = binding.configuration_ui.units
    assert (unit.id, unit.unit_id, unit.label) == ("changeType", "changeTypePicker", "Employee changes")
    assert "Every change" in unit.description and "restart the Worker" in unit.description
    [port] = unit.bindings
    assert (port.port, port.json_pointer) == ("changeType", "/changeType")
    with pytest.raises(ValueError, match=CONNECTION_NAME):
        EmployeeChangeFlow(Mock("other").connection())
