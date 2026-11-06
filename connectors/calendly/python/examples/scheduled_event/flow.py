# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Calendly scheduled event through a real Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.calendly import (
    Connection,
    GetScheduledEventInput,
    ScheduledEvent,
    new_get_scheduled_event_step,
)

CONNECTION_NAME = "calendly-scheduling"
FLOW_TYPE = "CalendlyScheduledEventPython"
EVENT = dex.Attribute("calendly-python-scheduled-event", ScheduledEvent)


# dex:group group-id:scheduled-event group-label:"Scheduled event"
# dex:explanation text:"Persist the scheduled event returned by Calendly."
class CompleteEventLookup(dex.Step[QueryResult[ScheduledEvent]]):
    """Complete the Flow with a safe event record."""

    def execute(self, context: dex.Context, input: QueryResult[ScheduledEvent]) -> dex.StepDecision:
        """Record the returned event and complete."""
        EVENT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ScheduledEventFlow(dex.Flow[GetScheduledEventInput]):
    """Look up one event using the configured Calendly connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("scheduled event example requires its static Calendly connection")
        self.complete = CompleteEventLookup()
        self.lookup = new_get_scheduled_event_step(
            step_type="GetScheduledEvent",
            step_input_type=GetScheduledEventInput,
            annotations={
                "groupId": "scheduled-event",
                "groupLabel": "Scheduled event",
                "explanation": "Read the selected scheduled event from Calendly.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow type."""
        return "CalendlyScheduledEventPython"

    def get_steps(self) -> dex.StepList[GetScheduledEventInput]:
        """Register the provider Query and completion Step."""
        return dex.StepList.start_step(self.lookup).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the event Attribute consumed by this Flow."""
        return dex.PersistenceSchema.of(EVENT)
