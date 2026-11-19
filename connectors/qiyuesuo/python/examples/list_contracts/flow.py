# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Qiyuesuo listContracts Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.qiyuesuo import (
    Connection,
    ListContractsInput,
    ListContractsOutput,
    new_list_contracts_step,
)

CONNECTION_NAME = "qiyuesuo"
_RESULT = dex.Attribute("qiyuesuo-result", ListContractsOutput)


def map_to_operation_input(request: ListContractsInput) -> ListContractsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:qiyuesuo group-label:"Qiyuesuo"
# dex:explanation text:"Start the Qiyuesuo query."
class _StartQuery(dex.Step[ListContractsInput]):
    def execute(self, context: dex.Context, input: ListContractsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:qiyuesuo group-label:"Qiyuesuo"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListContractsOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListContractsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListContractsFlow(dex.Flow[ListContractsInput]):
    """Run one Qiyuesuo query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Qiyuesuo Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_contracts_step(
            step_type="CallProvider",
            step_input_type=ListContractsInput,
            annotations={
                "groupId": "qiyuesuo",
                "groupLabel": "Qiyuesuo",
                "explanation": "Run Qiyuesuo listContracts.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListContractsQiyuesuo"

    def get_steps(self) -> dex.StepList[ListContractsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
