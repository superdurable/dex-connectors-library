# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read a Teams thread through a Dex Query Step."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.microsoft_teams import (
    Connection,
    ListThreadRepliesInput,
    ListThreadRepliesOutput,
    new_list_thread_replies_step,
)

CONNECTION_NAME = "microsoft-teams-incident"
FLOW_TYPE = "MicrosoftTeamsListThreadRepliesPython"
REPLIES = dex.Attribute("microsoft-teams-replies", ListThreadRepliesOutput)


@dataclass(frozen=True, slots=True)
class ThreadRequest:
    """Identify one Teams thread and bound its first page."""

    team_id: str = field(metadata={"dex_json_name": "teamId"})
    channel_id: str = field(metadata={"dex_json_name": "channelId"})
    message_id: str = field(metadata={"dex_json_name": "messageId"})

    def to_operation_input(self) -> ListThreadRepliesInput:
        """Request one page of replies from Graph."""
        return ListThreadRepliesInput(self.team_id, self.channel_id, self.message_id)


# dex:group group-id:teams-replies group-label:"Teams replies"
# dex:explanation text:"Store one bounded page of Teams replies and complete the Flow."
class CompleteThreadRead(dex.Step[QueryResult[ListThreadRepliesOutput]]):
    """Persist the read page for clients of this Flow."""

    def execute(self, context: dex.Context, input: QueryResult[ListThreadRepliesOutput]) -> dex.StepDecision:
        """Complete with the provider's reply page."""
        REPLIES.set(context, input.value)
        return dex.graceful_complete(input.value)


class ThreadRepliesFlow(dex.Flow[ThreadRequest]):
    """Read replies through the static Teams connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Teams replies require the static connection")
        self.complete = CompleteThreadRead()
        self.read = new_list_thread_replies_step(
            step_type="ReadMicrosoftTeamsThreadReplies",
            step_input_type=ThreadRequest,
            annotations={
                "groupId": "teams-replies",
                "groupLabel": "Teams replies",
                "explanation": "Read one bounded page of a Teams thread.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=ThreadRequest.to_operation_input,
            read=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ThreadRequest]:
        """Register the Teams Query and completion Step."""
        return dex.StepList.start_step(self.read).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the reply page Attribute."""
        return dex.PersistenceSchema.of(REPLIES)
