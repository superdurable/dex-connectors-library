# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List one Hiver shared inbox page through a durable Dex Query."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.hiver import Connection, ListInboxesInput, ListInboxesOutput, new_list_inboxes_step

CONNECTION_NAME = "hiver-support"
FLOW_TYPE = "HiverInboxPagePython"


@dataclass(frozen=True, slots=True)
class InboxPageRequest:
    """Optional Hiver page token and size supplied by the Flow caller."""

    page_token: str = ""
    page_size: int = 50


def map_to_list_inboxes_input(request: InboxPageRequest) -> ListInboxesInput:
    """Select one Hiver inbox page."""
    return ListInboxesInput(request.page_token, request.page_size)


# dex:group group-id:hiver-inboxes group-label:"Hiver inboxes"
# dex:explanation text:"Complete the Flow with one Hiver shared inbox page."
class CompleteInboxPage(dex.Step[QueryResult[ListInboxesOutput]]):
    """Complete with one credential-free inbox page."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "CompleteHiverInboxPage"

    def execute(self, context: dex.Context, input: QueryResult[ListInboxesOutput]) -> dex.StepDecision:
        """Return the page from the provider Query."""
        return dex.graceful_complete(input.value)


class InboxPageFlow(dex.Flow[InboxPageRequest]):
    """List a page through the static Hiver connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Hiver inbox Flow requires its static connection name")
        self.complete = CompleteInboxPage()
        self.list_inboxes = new_list_inboxes_step(
            step_type="ListHiverInboxes",
            step_input_type=InboxPageRequest,
            annotations={
                "groupId": "hiver-inboxes",
                "groupLabel": "Hiver inboxes",
                "explanation": "Read one page of Hiver shared inboxes using the configured API key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_list_inboxes_input,
            listed=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[InboxPageRequest]:
        """Register the provider Query and completion Step."""
        return dex.StepList.start_step(self.list_inboxes).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist no Attributes; the completion carries the safe page."""
        return dex.PersistenceSchema.of()
