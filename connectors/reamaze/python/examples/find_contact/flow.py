# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Look up one customer contact through the static Re:amaze connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.reamaze import (
    Connection,
    FindContactByEmailInput,
    FindContactByEmailOutput,
    new_find_contact_by_email_step,
)

CONNECTION_NAME = "reamaze-support"
FLOW_TYPE = "ReamazeFindContactPython"
CONTACT_RESULT = dex.Attribute("reamaze-contact-result", FindContactByEmailOutput)


# dex:group group-id:contact-lookup group-label:"Contact lookup"
# dex:explanation text:"Save the matching Re:amaze contact and complete the lookup."
class CompleteContactLookup(dex.Step[QueryResult[FindContactByEmailOutput]]):
    """Persist a safe contact result and complete the Flow."""

    def execute(self, context: dex.Context, input: QueryResult[FindContactByEmailOutput]) -> dex.StepDecision:
        """Store the contact page for inspection in Dex Web."""
        CONTACT_RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class FindContactFlow(dex.Flow[FindContactByEmailInput]):
    """Resolve an email address to its Re:amaze contact."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("contact lookup requires the static Re:amaze connection")
        self.complete = CompleteContactLookup()
        self.find = new_find_contact_by_email_step(
            step_type="FindReamazeContact",
            step_input_type=FindContactByEmailInput,
            annotations={
                "groupId": "contact-lookup",
                "groupLabel": "Contact lookup",
                "explanation": "Find the customer whose email exactly matches the request.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the registered Flow identity stable."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[FindContactByEmailInput]:
        """Register the Query and successful completion Steps."""
        return dex.StepList.start_step(self.find).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the result Attribute used by the completion Step."""
        return dex.PersistenceSchema.of(CONTACT_RESULT)
