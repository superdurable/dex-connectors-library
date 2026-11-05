# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Search an IMAP mailbox with one durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.superdurable_email import (
    Connection,
    SearchMessagesInput,
    SearchMessagesOutput,
    new_search_messages_step,
)

CONNECTION_NAME = "email-support"
FLOW_TYPE = "EmailMailboxSearchPython"


def map_to_search_input(value: SearchMessagesInput) -> SearchMessagesInput:
    """Use the complete typed start input for IMAP search."""
    return value


# dex:group group-id:email-search group-label:"Mailbox search"
# dex:explanation text:"Complete the Flow with the matching mailbox summaries."
class CompleteMailboxSearch(dex.Step[QueryResult[SearchMessagesOutput]]):
    """Return safe message summaries without credentials or message bodies."""

    def get_step_type(self) -> str:
        """Use a stable completion Step type."""
        return "CompleteEmailMailboxSearch"

    def execute(self, context: dex.Context, input: QueryResult[SearchMessagesOutput]) -> dex.StepDecision:
        """Complete the Flow after a successful IMAP search."""
        return dex.graceful_complete(input.value)


class MailboxSearchFlow(dex.Flow[SearchMessagesInput]):
    """Search the static email connection by IMAP headers."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("mailbox search requires its static email connection name")
        self.complete = CompleteMailboxSearch()
        self.search = new_search_messages_step(
            step_type="SearchEmailMailbox",
            step_input_type=SearchMessagesInput,
            annotations={
                "groupId": "email-search",
                "groupLabel": "Mailbox search",
                "explanation": "Search the configured IMAP mailbox for matching messages.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_search_input,
            searched=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow type."""
        return "EmailMailboxSearchPython"

    def get_steps(self) -> dex.StepList[SearchMessagesInput]:
        """Register the search and completion Steps."""
        return dex.StepList.start_step(self.search).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """This Flow persists no Attributes or Streams."""
        return dex.PersistenceSchema.of()
