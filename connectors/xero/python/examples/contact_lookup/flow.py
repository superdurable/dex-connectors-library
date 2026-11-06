# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Find one Xero contact through a Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.xero import (
    Connection,
    FindContactByEmailInput,
    FindContactByEmailOutput,
    new_find_contact_by_email_step,
)

CONNECTION_NAME = "xero-books"
FLOW_TYPE = "XeroFindContactByEmailPython"


# dex:group group-id:contact-lookup group-label:"Contact Lookup"
# dex:explanation text:"Return the one active Xero contact found by email."
class CompleteContactLookup(dex.Step[QueryResult[FindContactByEmailOutput]]):
    """Complete the Flow with the resolved contact."""

    def execute(self, context: dex.Context, input: QueryResult[FindContactByEmailOutput]) -> dex.StepDecision:
        """Return the provider result without persisting credentials."""
        return dex.graceful_complete(input.value)


class ContactLookupFlow(dex.Flow[FindContactByEmailInput]):
    """Find an active contact by a bare email address."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Xero connection at Worker registration."""
        self.complete = CompleteContactLookup()
        self.find = new_find_contact_by_email_step(
            step_type="FindContactByEmail",
            step_input_type=FindContactByEmailInput,
            annotations={
                "groupId": "contact-lookup",
                "groupLabel": "Contact Lookup",
                "explanation": "Ask Xero for the active contact with this email address.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow type."""
        return "XeroFindContactByEmailPython"

    def get_steps(self) -> dex.StepList[FindContactByEmailInput]:
        """Route only the happy-path found branch."""
        return dex.StepList.start_step(self.find).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Declare the empty persistence schema for this read-only Flow."""
        return dex.PersistenceSchema.of()
