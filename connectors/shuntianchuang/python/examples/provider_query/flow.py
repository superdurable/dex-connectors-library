# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Shuntianchuang provider data in a durable Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.shuntianchuang import Connection, ListModelsInput, ListModelsOutput, new_list_models_step

CONNECTION_NAME = "shuntianchuang"
_RESULT = dex.Attribute("shuntianchuang-query-result", ListModelsOutput)


def map_to_operation_input(input: ListModelsInput) -> ListModelsInput:
    """Pass the application Query input to the provider."""
    return input


# dex:group group-id:shuntianchuang group-label:"Shuntianchuang"
# dex:explanation text:"Start the Shuntianchuang Query."
class _StartQuery(dex.Step[ListModelsInput]):
    def get_step_type(self) -> str:
        return "StartShuntianchuangQuery"

    def execute(self, context: dex.Context, input: ListModelsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallShuntianchuang"), input)


# dex:group group-id:shuntianchuang group-label:"Shuntianchuang"
# dex:explanation text:"Persist the Shuntianchuang Query result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[ListModelsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteShuntianchuangQuery"

    def execute(self, context: dex.Context, input: QueryResult[ListModelsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProviderQueryFlow(dex.Flow[ListModelsInput]):
    """Read the configured Shuntianchuang account through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ProviderQueryFlow requires the static Shuntianchuang connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_models_step(
            step_type="CallShuntianchuang",
            step_input_type=ListModelsInput,
            annotations={
                "groupId": "shuntianchuang",
                "groupLabel": "Shuntianchuang",
                "explanation": "Read Shuntianchuang account data.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Shuntianchuang Query Flow identity."""
        return "ShuntianchuangProviderQuery"

    def get_steps(self) -> dex.StepList[ListModelsInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider Query output."""
        return dex.PersistenceSchema.of(_RESULT)
