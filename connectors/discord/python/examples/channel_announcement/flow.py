# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Create one mention-safe announcement in a Discord channel."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.discord import Connection, CreateMessageInput, Message, new_create_message_step

CONNECTION_NAME = "discord-announcements"
FLOW_TYPE = "DiscordChannelAnnouncementPython"


@dataclass(frozen=True, slots=True)
class AnnouncementInput:
    """Safe channel and announcement text supplied at Flow start."""

    channel_id: str = field(metadata={"dex_json_name": "channelId"})
    content: str

    def to_operation_input(self) -> CreateMessageInput:
        """Map durable start fields to the Discord message input."""
        return CreateMessageInput(self.channel_id, self.content)


announcement_result = dex.Attribute("discord-announcement-result", Message)


# dex:group group-id:discord-announcement group-label:"Discord announcement"
# dex:explanation text:"Store the sent Discord message after the provider confirms creation."
class CompleteAnnouncement(dex.Step[MutationResult[Message]]):
    """Save the safe message and complete the Flow."""

    def execute(self, context: dex.Context, input: MutationResult[Message]) -> dex.StepDecision:
        """Persist the resulting message without credentials."""
        announcement_result.set(context, input.value)
        return dex.graceful_complete(input.value)


class ChannelAnnouncementFlow(dex.Flow[AnnouncementInput]):
    """Send one channel announcement through a static Discord connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Discord announcement requires its static connection name")
        self.complete = CompleteAnnouncement()
        self.create = new_create_message_step(
            step_type="CreateDiscordAnnouncement",
            step_input_type=AnnouncementInput,
            annotations={
                "groupId": "discord-announcement",
                "groupLabel": "Discord announcement",
                "explanation": "Send one mention-safe announcement to Discord.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=AnnouncementInput.to_operation_input,
            created=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[AnnouncementInput]:
        """Register the provider mutation and completion Step."""
        return dex.StepList.start_step(self.create).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the safe completed message Attribute."""
        return dex.PersistenceSchema.of(announcement_result)
