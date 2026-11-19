# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable West list_domains Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.west import (
    Connection,
    ListDomainsInput,
    ListDomainsOutput,
    new_list_domains_step,
)

CONNECTION_NAME = "west"
_RESULT = dex.Attribute("west-result", ListDomainsOutput)


def map_to_operation_input(request: ListDomainsInput) -> ListDomainsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:west group-label:"West"
# dex:explanation text:"Start the West query."
class _StartQuery(dex.Step[ListDomainsInput]):
    def execute(self, context: dex.Context, input: ListDomainsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:west group-label:"West"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListDomainsOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListDomainsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListDomainsFlow(dex.Flow[ListDomainsInput]):
    """Run one West query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("West Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_domains_step(
            step_type="CallProvider",
            step_input_type=ListDomainsInput,
            annotations={
                "groupId": "west",
                "groupLabel": "West",
                "explanation": "Run West list_domains.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListDomainsWest"

    def get_steps(self) -> dex.StepList[ListDomainsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
