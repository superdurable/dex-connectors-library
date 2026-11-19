# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Search Enlvon policy cards through a Dex Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.enlvon import (
    Connection,
    SearchPoliciesInput,
    SearchPoliciesOutput,
    new_search_policies_step,
)

CONNECTION_NAME = "enlvon"
policy_cards_attribute = dex.Attribute("enlvon-policy-cards-python", SearchPoliciesOutput)


# dex:group group-id:policies group-label:"Policy directory"
# dex:explanation text:"Record source-linked policy cards and complete the Flow."
class RecordPolicyCards(dex.Step[QueryResult[SearchPoliciesOutput]]):
    """Persist title, date, source, and original government URL only."""

    def get_step_type(self) -> str:
        """Return the stable recording Step identity."""
        return "RecordEnlvonPolicyCards"

    def execute(
        self, context: dex.Context, input: QueryResult[SearchPoliciesOutput]
    ) -> dex.StepDecision:
        """Record the restricted card metadata."""
        policy_cards_attribute.set(context, input.value)
        return dex.graceful_complete(input.value)


class PolicyDirectoryFlow(dex.Flow[SearchPoliciesInput]):
    """Search the connected Enlvon policy corpus."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query and recording Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("policy directory Flow requires the static Enlvon connection")
        self.record = RecordPolicyCards()
        self.search = new_search_policies_step(
            step_type="SearchEnlvonPolicies",
            step_input_type=SearchPoliciesInput,
            annotations={
                "groupId": "policies",
                "groupLabel": "Policy directory",
                "explanation": "GET source-linked electricity policy cards.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the durable Flow identity."""
        return "EnlvonPolicyDirectoryPython"

    def get_steps(self) -> dex.StepList[SearchPoliciesInput]:
        """Register the provider and recording Steps."""
        return dex.StepList.start_step(self.search).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the restricted policy card Attribute."""
        return dex.PersistenceSchema.of(policy_cards_attribute)
