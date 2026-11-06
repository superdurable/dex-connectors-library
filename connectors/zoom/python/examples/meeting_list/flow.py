# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List one page of Zoom meetings through a named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zoom import Connection, ListMeetingsInput, MeetingPage, new_list_meetings_step

CONNECTION_NAME = "zoom-meetings"
FLOW_TYPE = "ZoomListMeetingsPython"


# dex:group group-id:meeting-list group-label:"Zoom meetings"
# dex:explanation text:"Complete with the authorized user's meeting page."
class MeetingsListed(dex.Step[QueryResult[MeetingPage]]):
    """Complete with one safe meeting page."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "MeetingsListed"

    def execute(self, context: dex.Context, input: QueryResult[MeetingPage]) -> dex.StepDecision:
        """Return the provider's meeting page to the caller."""
        return dex.graceful_complete(input.value)


class MeetingListFlow(dex.Flow[ListMeetingsInput]):
    """Read one Zoom meeting page from the authorized user's account."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Zoom connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("meeting list Flow requires zoom-meetings connection")
        self.listed = MeetingsListed()
        self.list_meetings = new_list_meetings_step(
            step_type="ListMeetings",
            step_input_type=ListMeetingsInput,
            annotations={
                "groupId": "meeting-list",
                "groupLabel": "Zoom meetings",
                "explanation": "List the authorized user's selected meeting page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            listed=self.listed,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "ZoomListMeetingsPython"

    def get_steps(self) -> dex.StepList[ListMeetingsInput]:
        """Register the Query and its happy-path completion Step."""
        return dex.StepList.start_step(self.list_meetings).other_steps(self.listed)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Return an empty schema because this Flow stores no Attributes."""
        return dex.PersistenceSchema.of()
