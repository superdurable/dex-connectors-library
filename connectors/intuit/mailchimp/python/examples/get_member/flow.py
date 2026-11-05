# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read and retain one Mailchimp audience contact in a Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.intuit_mailchimp import (
    Connection,
    GetMemberInput,
    Member,
    new_get_member_step,
)

CONNECTION_NAME = "mailchimp-audience"
FLOW_TYPE = "MailchimpGetMemberPython"
member_result = dex.Attribute("member-result", Member)


# dex:group group-id:audience group-label:"Audience"
# dex:explanation text:"Retain the contact returned by Mailchimp."
class CompleteMemberRead(dex.Step[QueryResult[Member]]):
    """Persist a safe contact summary after the provider query."""

    def execute(self, context: dex.Context, input: QueryResult[Member]) -> dex.StepDecision:
        """Complete the Flow with the returned contact."""
        member_result.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetMemberFlow(dex.Flow[GetMemberInput]):
    """Read one audience contact through the declared Mailchimp connection."""

    def __init__(self, connection: Connection) -> None:
        if connection is None:
            raise ValueError("Mailchimp connection is required")
        self.complete = CompleteMemberRead()
        self.get_member = new_get_member_step(
            step_type="GetMember",
            step_input_type=GetMemberInput,
            annotations={
                "groupId": "audience",
                "groupLabel": "Audience",
                "explanation": "Read one contact from a Mailchimp audience.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetMemberInput]:
        """Start the Mailchimp query and complete on its happy path."""
        return dex.StepList.start_step(self.get_member).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the safe contact Attribute used by completion."""
        return dex.PersistenceSchema.of(member_result)
