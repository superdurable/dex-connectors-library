# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one bounded, newest-first page of a Slack channel's top-level messages through a Dex Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.slack import (
    Connection,
    ListChannelMessagesInput,
    ListChannelMessagesOutput,
    new_list_channel_messages_step,
)

CONNECTION_NAME = "slack-channel-history-reader"

channel_page = dex.Attribute("slack-python-channel-history-page", ListChannelMessagesOutput)


# dex:group group-id:channel-history group-label:"Channel History"
# dex:explanation text:"Persist the Slack channel history page and complete."
class CompleteChannelPage(dex.Step[QueryResult[ListChannelMessagesOutput]]):
    """Persist the page and complete the Flow with it."""

    def execute(
        self, context: dex.Context, input: QueryResult[ListChannelMessagesOutput]
    ) -> dex.StepDecision:
        """Complete with the page; pass its nextCursor back as cursor to read older messages."""
        channel_page.set(context, input.value)
        return dex.graceful_complete(input.value)


class ChannelMessagesFlow(dex.Flow[ListChannelMessagesInput]):
    """Read one channel history page; the Flow input is the operation input."""

    def __init__(self, connection: Connection) -> None:
        """Bind the read Step to the static connection."""
        self.complete = CompleteChannelPage()
        self.read = new_list_channel_messages_step(
            step_type="ReadSlackChannelPage",
            step_input_type=ListChannelMessagesInput,
            annotations={
                "groupId": "channel-history",
                "groupLabel": "Channel History",
                "explanation": "Read one page of the channel's top-level messages, newest first.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=read_channel_page_input,
            read=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable example Flow identity."""
        return "SlackChannelMessagesPython"

    def get_steps(self) -> dex.StepList[ListChannelMessagesInput]:
        """Return the read and completion Steps."""
        return dex.StepList.start_step(self.read).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the page Attribute."""
        return dex.PersistenceSchema.of(channel_page)


def read_channel_page_input(input: ListChannelMessagesInput) -> ListChannelMessagesInput:
    """Pass the requested channel, range, page size, and cursor through unchanged."""
    return input
