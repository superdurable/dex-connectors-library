# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Jinshuju listFormsPage Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.jinshuju import (
    Connection,
    ListFormsPageInput,
    ListFormsPageOutput,
    new_list_forms_page_step,
)

CONNECTION_NAME = "jinshuju"
_RESULT = dex.Attribute("jinshuju-result", ListFormsPageOutput)


def map_to_operation_input(request: ListFormsPageInput) -> ListFormsPageInput:
    """Forward the provider request."""
    return request


# dex:group group-id:jinshuju group-label:"Jinshuju"
# dex:explanation text:"Start the Jinshuju query."
class _StartQuery(dex.Step[ListFormsPageInput]):
    def execute(self, context: dex.Context, input: ListFormsPageInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:jinshuju group-label:"Jinshuju"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListFormsPageOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListFormsPageOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListFormsPageFlow(dex.Flow[ListFormsPageInput]):
    """Run one Jinshuju query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Jinshuju Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_forms_page_step(
            step_type="CallProvider",
            step_input_type=ListFormsPageInput,
            annotations={
                "groupId": "jinshuju",
                "groupLabel": "Jinshuju",
                "explanation": "Run Jinshuju listFormsPage.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListFormsPageJinshuju"

    def get_steps(self) -> dex.StepList[ListFormsPageInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
