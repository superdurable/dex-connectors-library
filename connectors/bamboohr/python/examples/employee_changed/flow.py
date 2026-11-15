# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Flow per BambooHR employee change: record it and read an inserted or updated employee back."""

from dataclasses import dataclass, field
from datetime import datetime

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.bamboohr import (
    EMPLOYEE_CHANGE_ACTION_DELETED,
    EMPLOYEE_CHANGE_ACTION_INSERTED,
    EMPLOYEE_CHANGE_ACTION_UPDATED,
    Connection,
    EmployeeChangedEvent,
    EmployeeRecord,
    GetEmployeeInput,
    define_employee_changed_trigger_binding,
    new_get_employee_step,
)

CONNECTION_NAME = "bamboohr-company"
BINDING_NAME = "employee-changes"
FLOW_TYPE = "BambooHREmployeeChangePython"
FLOW_ID_PREFIX = "bamboohr-employee-change-"
HAND_OFF_FIELDS = (
    "firstName",
    "lastName",
    "workEmail",
    "status",
    "department",
    "jobTitle",
    "terminationDate",
)
"""The fields read back for an inserted or updated employee, because a change carries no field values."""


@dataclass(frozen=True, slots=True)
class EmployeeChangeInput:
    """One employeeChanged delivery: the stable Trigger event ID beside the delivered change."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: EmployeeChangedEvent


@dataclass(frozen=True, slots=True)
class EmployeeChangeOutcome:
    """The Flow result: the changed employee and, unless it is gone, its hand-off fields."""

    employee_id: str = field(metadata={"dex_json_name": "employeeId"})
    action: str
    last_changed: datetime = field(metadata={"dex_json_name": "lastChanged", "dex_json_format": "rfc3339"})
    current: EmployeeRecord | None = field(default=None, metadata={"dex_json_omit_default": True})
    is_gone: bool = field(default=False, metadata={"dex_json_name": "isGone", "dex_json_omit_default": True})


employee_change_attribute = dex.Attribute("bamboohr-employee-change", EmployeeChangeInput)
employee_change_outcome_attribute = dex.Attribute("bamboohr-employee-change-outcome", EmployeeChangeOutcome)


def resolve_flow_id(event: TriggerEvent[EmployeeChangedEvent]) -> str:
    """Name one Flow per change, so a redelivered change finds its existing Flow."""
    return FLOW_ID_PREFIX + event.id


def map_to_flow_input(event: TriggerEvent[EmployeeChangedEvent]) -> EmployeeChangeInput:
    """Keep the Trigger event ID beside the delivered change."""
    return EmployeeChangeInput(event.id, event.payload)


def accepts_every_employee_change(event: TriggerEvent[EmployeeChangedEvent]) -> bool:
    """Admit every change; the binding's saved change type already narrows the list."""
    return True


def map_to_get_employee_input(input: EmployeeChangeInput) -> GetEmployeeInput:
    """Read the changed employee's hand-off fields."""
    return GetEmployeeInput(employee_id=input.change.employee_id, fields=HAND_OFF_FIELDS)


def validate_employee_change_input(input: EmployeeChangeInput) -> str:
    """Return why a delivered change is unusable, or "" when it is valid."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not input.change.employee_id.strip() or input.change.last_changed.year == 1:
        return "change.employeeId and change.lastChanged are required"
    if input.change.action not in (
        EMPLOYEE_CHANGE_ACTION_INSERTED,
        EMPLOYEE_CHANGE_ACTION_UPDATED,
        EMPLOYEE_CHANGE_ACTION_DELETED,
    ):
        return "change.action must be Inserted, Updated, or Deleted"
    return ""


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the employee read back, or mark it gone when deleted first, and complete."
class CompleteEmployeeChange(dex.Step[QueryResult[EmployeeRecord]]):
    """Record the read-back outcome and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteEmployeeChange"

    def execute(self, context: dex.Context, input: QueryResult[EmployeeRecord]) -> dex.StepDecision:
        """Keep the current employee only when BambooHR found it."""
        change = employee_change_attribute.get(context).change
        is_found = input.branch == "found"
        outcome = EmployeeChangeOutcome(
            change.employee_id,
            change.action,
            change.last_changed,
            input.value if is_found else None,
            not is_found,
        )
        employee_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the change; complete at once for a deleted employee, otherwise read it back."
class RecordEmployeeChange(dex.Step[EmployeeChangeInput]):
    """Validate and record the delivered change."""

    def __init__(self, read_changed_employee: dex.StepTarget[EmployeeChangeInput]) -> None:
        """Bind the read-back Step this Step continues to."""
        self.read_changed_employee = read_changed_employee

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordEmployeeChange"

    def execute(self, context: dex.Context, input: EmployeeChangeInput) -> dex.StepDecision:
        """Complete at once for a deletion, which BambooHR no longer returns; read anything else back."""
        problem = validate_employee_change_input(input)
        if problem:
            return dex.force_fail(problem)
        employee_change_attribute.set(context, input)
        change = input.change
        if change.action == EMPLOYEE_CHANGE_ACTION_DELETED:
            outcome = EmployeeChangeOutcome(
                change.employee_id, change.action, change.last_changed, is_gone=True
            )
            employee_change_outcome_attribute.set(context, outcome)
            return dex.graceful_complete(outcome)
        return dex.go_to(self.read_changed_employee, input)


class EmployeeChangeFlow(dex.Flow[EmployeeChangeInput]):
    """Record one BambooHR employee change and read an inserted or updated employee back."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static BambooHR connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError(f"employee change Flow requires the {CONNECTION_NAME} connection")
        self.complete = CompleteEmployeeChange()
        self.read_changed_employee = new_get_employee_step(
            step_type="ReadChangedEmployee",
            step_input_type=EmployeeChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed employee's hand-off fields back from BambooHR, since the "
                "change carries no field values.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_employee_input,
            found=self.complete,
            # Business reaction: an employee deleted after the change is recorded as gone.
            not_found=self.complete,
        )
        self.record = RecordEmployeeChange(self.read_changed_employee)

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "BambooHREmployeeChangePython"

    def get_steps(self) -> dex.StepList[EmployeeChangeInput]:
        """Start at the record Step, which the Trigger starts the Flow at."""
        return dex.StepList.start_step(self.record).other_steps(self.read_changed_employee, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(employee_change_attribute, employee_change_outcome_attribute)

    # Declares the employeeChanged binding and its change type picker; Dex reads one static return.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_employee_changed_trigger_binding(
                connection_name="bamboohr-company",
                binding_name="employee-changes",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "changeType",
                            "changeTypePicker",
                            "Employee changes",
                            "Choose which BambooHR employee changes start this Flow: Inserted for new hires,"
                            " Updated for record, job, compensation, and employment status changes including"
                            " terminations, Deleted for removed records, or Every change. The unit saves "
                            "inserted, updated, or deleted; leaving it unsaved or choosing Every change "
                            "starts a Flow for every change. The changes come from the employees the "
                            "connection's API key user can see, so no BambooHR page needs a value. The first"
                            " poll starts at the current time and earlier changes never start Flows; restart"
                            " the Worker after saving, and a changed choice starts again from the current "
                            "time.",
                            False,
                            (ConnectorUIBinding("changeType", "/changeType"),),
                        ),
                    )
                ),
            )
        ]
