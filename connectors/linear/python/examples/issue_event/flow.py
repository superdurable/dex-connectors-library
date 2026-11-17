# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Record each verified Linear Issue webhook in a Dex Flow."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.linear import Connection, IssueEvent, define_issue_event_received_trigger_binding

CONNECTION_NAME = "linear-workspace"
BINDING_NAME = "issue-events"
FLOW_ID_PREFIX = "linear-issue-event-"


@dataclass(frozen=True, slots=True)
class IssueEventInput:
    """One signed Linear Issue event delivered to the Flow."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    received: IssueEvent


issue_event_attribute = dex.Attribute("linear-issue-event-python", IssueEventInput)


def resolve_flow_id(event: TriggerEvent[IssueEvent]) -> str:
    """Map the stable action, issue UUID, and updated time to one Flow."""
    return FLOW_ID_PREFIX + event.id.replace(":", "-")


def map_to_flow_input(event: TriggerEvent[IssueEvent]) -> IssueEventInput:
    """Keep the delivery ID with its verified Issue payload."""
    return IssueEventInput(event.id, event.payload)


# dex:group group-id:issue-event group-label:"Issue event"
# dex:explanation text:"Record the verified Linear Issue event and complete the Flow."
class RecordIssueEvent(dex.Step[IssueEventInput]):
    """Store a verified Issue event in a Dex Attribute."""

    def get_step_type(self) -> str:
        """Keep the Step identity stable."""
        return "RecordIssueEvent"

    def execute(self, context: dex.Context, input: IssueEventInput) -> dex.StepDecision:
        """Fail an incomplete event; otherwise record and complete."""
        if not input.trigger_event_id or not input.received.issue.id:
            return dex.force_fail("triggerEventId and received.issue.id are required")
        issue_event_attribute.set(context, input)
        return dex.graceful_complete(input)


class IssueEventFlow(dex.Flow[IssueEventInput]):
    """Record one verified Linear Issue event per stable event ID."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Linear connection."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Issue event Flow requires the static Linear connection")
        self.record = RecordIssueEvent()

    def get_flow_type(self) -> str:
        """Keep the Flow identity stable."""
        return "LinearIssueEventPython"

    def get_steps(self) -> dex.StepList[IssueEventInput]:
        """Start at the recording Step."""
        return dex.StepList.start_step(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the Issue event Attribute."""
        return dex.PersistenceSchema.of(issue_event_attribute)

    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_issue_event_received_trigger_binding(
                connection_name="linear-workspace",
                binding_name="issue-events",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "team",
                            "teamPicker",
                            "Team",
                            "Choose the team whose Issue events start this Flow. The OAuth picker lists "
                            "accessible teams and stores the selected team UUID. For a personal key, "
                            "start at https://linear.app/settings/api, then open Settings > Teams > "
                            "the team > Copy team ID and paste its UUID. This is not a secret. "
                            "Leave it empty for all teams subscribed by the webhook.",
                            False,
                            (ConnectorUIBinding("teamId", "/teamId"),),
                        ),
                    )
                ),
            )
        ]
