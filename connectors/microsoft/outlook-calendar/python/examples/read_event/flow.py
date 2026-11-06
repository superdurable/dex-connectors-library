# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Outlook event through the static Dex Web connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.microsoft_outlook_calendar import Connection, Event, GetEventInput, new_get_event_step

CONNECTION_NAME = "outlook-calendar-scheduler"


# dex:group group-id:outlook-calendar group-label:"Outlook Calendar"
# dex:explanation text:"Complete after Outlook returns the requested event."
class _EventFound(dex.Step[QueryResult[Event]]):
    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: QueryResult[Event]
    ) -> dex.StepDecision:
        """Return the event without credentials or token material."""
        return dex.graceful_complete(input.value)


class ReadOutlookEventFlow(dex.Flow[GetEventInput]):
    """Read one event by ID using the named Outlook Calendar connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Outlook event read requires its static connection name")
        self.found = _EventFound()
        # dex:group group-id:outlook-calendar group-label:"Outlook Calendar"
        # dex:explanation text:"Read one Outlook event with the connection's mailbox access."
        self.read = new_get_event_step(
            step_type="ReadOutlookEvent",
            step_input_type=GetEventInput,
            annotations={
                "groupId": "outlook-calendar",
                "groupLabel": "Outlook Calendar",
                "explanation": "Read one Outlook event with the connection's mailbox access.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            found=self.found,
        )

    def get_flow_type(self) -> str:
        """Return the stable example Flow identity."""
        return "ReadOutlookEvent"

    def get_steps(self) -> dex.StepList[GetEventInput]:
        """Register the Graph read and completion Step."""
        return dex.StepList.start_step(self.read).other_steps(self.found)
