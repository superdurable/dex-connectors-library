# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one TokenGP response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.tokengp import (
    Connection,
    ListModelsInput,
    ListModelsOutput,
    new_list_models_step,
)

CONNECTION_NAME = "tokengp"
_RESULT = dex.Attribute("tokengp-query-result", ListModelsOutput)


def map_to_operation_input(input: ListModelsInput) -> ListModelsInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the TokenGP provider Query."
class _StartQuery(dex.Step[ListModelsInput]):
    def get_step_type(self) -> str:
        return "StartListModelsQuery"

    def execute(self, context: dex.Context, input: ListModelsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListModelsQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated TokenGP Query result."
class _CompleteQuery(dex.Step[QueryResult[ListModelsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListModelsQuery"

    def execute(self, context: dex.Context, input: QueryResult[ListModelsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TokenGPListModelsFlow(dex.Flow[ListModelsInput]):
    """Run the TokenGP Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("TokenGP Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_list_models_step(
            step_type="ListModelsQuery",
            step_input_type=ListModelsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "TokenGP",
                "explanation": "Call TokenGP ListModels using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "TokenGPListModels"

    def get_steps(self) -> dex.StepList[ListModelsInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
