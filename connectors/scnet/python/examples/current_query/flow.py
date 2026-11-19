# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read SCNet provider data through a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.scnet import Connection, ListModelsInput, ListModelsOutput, new_list_models_step

CONNECTION_NAME = "scnet"
_RESULT = dex.Attribute("scnet-python-query-result", ListModelsOutput)


def map_to_operation_input(input: ListModelsInput) -> ListModelsInput:
    """Forward the provider Query input."""
    return input


# dex:group group-id:scnet group-label:"SCNet"
# dex:explanation text:"Start the SCNet provider Query."
class _StartQuery(dex.Step[ListModelsInput]):
    def get_step_type(self) -> str:
        return "StartSCNet"

    def execute(self, context: dex.Context, input: ListModelsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallSCNet"), input)


# dex:group group-id:scnet group-label:"SCNet"
# dex:explanation text:"Persist the SCNet Query output and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[ListModelsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteSCNet"

    def execute(self, context: dex.Context, input: QueryResult[ListModelsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListModelsFlow(dex.Flow[ListModelsInput]):
    """Read SCNet through the static connection and persist its result."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("SCNet Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_models_step(
            step_type="CallSCNet",
            step_input_type=ListModelsInput,
            annotations={
                "groupId": "scnet",
                "groupLabel": "SCNet",
                "explanation": "Read SCNet provider data.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable SCNet Flow identity."""
        return "SCNetListModels"

    def get_steps(self) -> dex.StepList[ListModelsInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
