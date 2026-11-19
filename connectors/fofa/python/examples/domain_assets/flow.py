# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Search public indexed hosts for one domain."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.fofa import (
    Connection,
    SearchDomainAssetsInput,
    SearchDomainAssetsOutput,
    new_search_domain_assets_step,
)

CONNECTION_NAME = "fofa"
_OUTCOME = dex.Attribute("fofa-domain-assets-outcome", SearchDomainAssetsOutput)


def map_to_operation_input(input: SearchDomainAssetsInput) -> SearchDomainAssetsInput:
    """Pass the requested value to the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the provider query."
class _StartQuery(dex.Step[SearchDomainAssetsInput]):
    def get_step_type(self) -> str:
        return "StartDomainAssets"

    def execute(self, context: dex.Context, input: SearchDomainAssetsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("SearchDomainAssetsQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[SearchDomainAssetsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteDomainAssets"

    def execute(self, context: dex.Context, input: QueryResult[SearchDomainAssetsOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class FOFADomainAssetsFlow(dex.Flow[SearchDomainAssetsInput]):
    """Run fofa's Query with its static project connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("example requires the static fofa connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_search_domain_assets_step(
            step_type="SearchDomainAssetsQuery",
            step_input_type=SearchDomainAssetsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "fofa",
                "explanation": "Call fofa SearchDomainAssets.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "FOFADomainAssetsFlow"

    def get_steps(self) -> dex.StepList[SearchDomainAssetsInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
