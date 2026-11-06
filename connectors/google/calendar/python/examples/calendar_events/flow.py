# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List a bounded Google Calendar window through a Dex Query."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_calendar import Connection, ListEventsInput, ListEventsOutput, new_list_events_step

CONNECTION_NAME = "google-calendar-events-reader"


@dataclass(frozen=True, slots=True)
class CalendarWindowInput:
    """Calendar and explicit window to inspect."""

    calendar_id: str = field(metadata={"dex_json_name": "calendarId"})
    time_min: str = field(metadata={"dex_json_name": "timeMin"})
    time_max: str = field(metadata={"dex_json_name": "timeMax"})

    def to_operation_input(self) -> ListEventsInput:
        """Map the Flow input to one bounded provider request."""
        return ListEventsInput(self.calendar_id, self.time_min, self.time_max)


listed_events = dex.Attribute("google-calendar-python-listed-events", ListEventsOutput)


# dex:group group-id:google-calendar group-label:"Google Calendar"
# dex:explanation text:"Persist the listed events and complete the event listing Flow."
class CompleteCalendarPage(dex.Step[QueryResult[ListEventsOutput]]):
    """Store public event data and return the checked page."""

    def execute(self, context: dex.Context, input: QueryResult[ListEventsOutput]) -> dex.StepDecision:
        """Complete the Flow after a successful Calendar read."""
        listed_events.set(context, input.value)
        return dex.graceful_complete(input.value)


class CalendarEventsFlow(dex.Flow[CalendarWindowInput]):
    """Route listEvents through the static Google Calendar connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query before Worker registration."""
        self.complete = CompleteCalendarPage()
        self.list_events = new_list_events_step(
            step_type="ListCalendarEvents",
            step_input_type=CalendarWindowInput,
            annotations={
                "groupId": "google-calendar",
                "groupLabel": "Google Calendar",
                "explanation": "List events in the requested bounded window.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=CalendarWindowInput.to_operation_input,
            listed=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable example Flow identity."""
        return "GoogleCalendarEventsPython"

    def get_steps(self) -> dex.StepList[CalendarWindowInput]:
        """Start with the provider Query and complete on its happy path."""
        return dex.StepList.start_step(self.list_events).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the public events Attribute consumed by this Flow."""
        return dex.PersistenceSchema.of(listed_events)
