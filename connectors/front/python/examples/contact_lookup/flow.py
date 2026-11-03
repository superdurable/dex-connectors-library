# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Find a Front contact through one durable Dex Query Step."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.front import (
    Connection,
    FindContactByEmailInput,
    FindContactByEmailOutput,
    new_find_contact_by_email_step,
)

CONNECTION_NAME = "front-support"
FLOW_TYPE = "FrontContactLookupPython"


@dataclass(frozen=True, slots=True)
class ContactLookupRequest:
    """Customer email entered when starting the Flow."""

    email: str


def map_to_contact_lookup_input(request: ContactLookupRequest) -> FindContactByEmailInput:
    """Map the safe Flow input to Front's email alias lookup."""
    return FindContactByEmailInput(email=request.email)


# dex:group group-id:front-contact group-label:"Front contact"
# dex:explanation text:"Return the Front contact found for the requested email."
class CompleteContactLookup(dex.Step[QueryResult[FindContactByEmailOutput]]):
    """Complete the Flow with the contact lookup output."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "CompleteFrontContactLookup"

    def execute(self, context: dex.Context, input: QueryResult[FindContactByEmailOutput]) -> dex.StepDecision:
        """Keep the result free of connection credentials."""
        return dex.graceful_complete(input.value)


class ContactLookupFlow(dex.Flow[ContactLookupRequest]):
    """Find a contact through the static Front connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Front contact lookup requires its static connection name")
        self.complete = CompleteContactLookup()
        self.lookup = new_find_contact_by_email_step(
            step_type="FindFrontContactByEmail",
            step_input_type=ContactLookupRequest,
            annotations={
                "groupId": "front-contact",
                "groupLabel": "Front contact",
                "explanation": "Find the Front contact with this email handle.",
            },
            connection=connection,
            connection_name="front-support",
            map_to_operation_input=map_to_contact_lookup_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable application Flow type."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ContactLookupRequest]:
        """Register the provider Query and completion Step."""
        return dex.StepList.start_step(self.lookup).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist no Attributes; completion carries the safe contact."""
        return dex.PersistenceSchema.of()
