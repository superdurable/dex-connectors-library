# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read ppio through one durable provider Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.ppio import Connection, ListModelsInput, ListModelsOutput, new_list_models_step

CONNECTION_NAME = "ppio"
_OUTCOME = dex.Attribute("ppio-list_models-outcome", ListModelsOutput)


def map_to_operation_input(input: ListModelsInput) -> ListModelsInput:
    """Forward the typed Flow input to the provider Query."""
    return input


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Begin the ppio read with the configured connection."
class _Start(dex.Step[ListModelsInput]):
    def get_step_type(self) -> str:
        return "StartListModels"

    def execute(self, context: dex.Context, input: ListModelsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListModelsQuery"), input)


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[ListModelsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListModels"

    def execute(self, context: dex.Context, input: QueryResult[ListModelsOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListModelsFlow(dex.Flow[ListModelsInput]):
    """Durably read ppio with its static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ppio Flow requires its static connection")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_list_models_step(
            step_type="ListModelsQuery",
            step_input_type=ListModelsInput,
            annotations={"groupId": "provider", "groupLabel": "ppio", "explanation": "Call ppio listModels."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "PpioListModels"

    def get_steps(self) -> dex.StepList[ListModelsInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
