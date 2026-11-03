# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Get a Trello card through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.atlassian_trello import Card, Connection, GetCardInput, new_get_card_step

CONNECTION_NAME = "trello-requests"
FLOW_TYPE = "TrelloApprovedRequestCardPython"


def card_request(input: GetCardInput) -> GetCardInput:
    """Pass the stable card ID to the connector Query."""
    return input


# dex:group group-id:trello-card group-label:"Trello card"
# dex:explanation text:"Return the requested Trello card after a successful provider read."
class CompleteCardRead(dex.Step[QueryResult[Card]]):
    """Return the card without retaining provider credentials in Flow state."""

    def get_step_type(self) -> str:
        """Keep this application Step identity stable."""
        return "CompleteTrelloCardRead"

    def execute(self, context: dex.Context, input: QueryResult[Card]) -> dex.StepDecision:
        """Complete the Flow with the safe card view."""
        return dex.graceful_complete(input.value)


class ApprovedRequestCardFlow(dex.Flow[GetCardInput]):
    """Read a card using the static Trello connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the approved request Flow requires its static Trello connection")
        self.complete = CompleteCardRead()
        self.get_card = new_get_card_step(
            step_type="GetApprovedRequestCard",
            step_input_type=GetCardInput,
            annotations={
                "groupId": "trello-card",
                "groupLabel": "Trello card",
                "explanation": "Read the approved request card by Trello ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=card_request,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow type used by Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetCardInput]:
        """Register the provider Query and completion Steps."""
        return dex.StepList.start_step(self.get_card).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Declare that this read-only Flow has no persisted Attributes."""
        return dex.PersistenceSchema.of()
