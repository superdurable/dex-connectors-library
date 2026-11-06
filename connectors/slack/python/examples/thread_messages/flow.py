# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Post one channel message, then read back its thread through Dex Steps."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.slack import (
    Connection,
    ListThreadMessagesInput,
    ListThreadMessagesOutput,
    PostChannelMessageInput,
    PostMessageOutput,
    new_list_thread_messages_step,
    new_post_channel_message_step,
)

CONNECTION_NAME = "slack-thread-messages"


@dataclass(frozen=True, slots=True)
class ThreadMessageInput:
    """Channel and text supplied to the Flow."""

    channel_id: str = field(metadata={"dex_json_name": "channelId"})
    text: str

    def to_post_input(self) -> PostChannelMessageInput:
        """Map the Flow request to Slack's post operation."""
        return PostChannelMessageInput(self.channel_id, self.text)


thread_page = dex.Attribute("slack-python-thread-page", ListThreadMessagesOutput)


# dex:group group-id:slack-thread group-label:"Slack Thread"
# dex:explanation text:"Persist the Slack thread page after posting the channel message."
class CompleteThread(dex.Step[QueryResult[ListThreadMessagesOutput]]):
    """Persist public message data and complete the Flow."""

    def execute(self, context: dex.Context, input: QueryResult[ListThreadMessagesOutput]) -> dex.StepDecision:
        """Complete with the bounded page."""
        thread_page.set(context, input.value)
        return dex.graceful_complete(input.value)


class ThreadMessagesFlow(dex.Flow[ThreadMessageInput]):
    """Post a channel message and verify it through the thread Query."""

    def __init__(self, connection: Connection) -> None:
        """Bind both operation Steps to one static connection."""
        self.complete = CompleteThread()
        self.read = new_list_thread_messages_step(
            step_type="ListPostedThreadMessages",
            step_input_type=MutationResult[PostMessageOutput],
            annotations={
                "groupId": "slack-thread",
                "groupLabel": "Slack Thread",
                "explanation": "Read the posted message's thread.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda result: ListThreadMessagesInput(
                result.value.message.channel_id, result.value.message.timestamp, page_size=1
            ),
            read=self.complete,
        )
        self.post = new_post_channel_message_step(
            step_type="PostChannelMessage",
            step_input_type=ThreadMessageInput,
            annotations={
                "groupId": "slack-thread",
                "groupLabel": "Slack Thread",
                "explanation": "Post the requested channel message once.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=ThreadMessageInput.to_post_input,
            sent=self.read,
        )

    def get_flow_type(self) -> str:
        """Return the stable example Flow identity."""
        return "SlackThreadMessagesPython"

    def get_steps(self) -> dex.StepList[ThreadMessageInput]:
        """Return the post, read, and completion Steps."""
        return dex.StepList.start_step(self.post).other_steps(self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the page Attribute consumed by the Flow."""
        return dex.PersistenceSchema.of(thread_page)
