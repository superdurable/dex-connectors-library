# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Mingdao response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.mingdao import (
    Connection,
    ListWorksheetsInput,
    ListWorksheetsOutput,
    new_list_worksheets_step,
)

CONNECTION_NAME = "mingdao"
_RESULT = dex.Attribute("mingdao-query-result", ListWorksheetsOutput)


def map_to_operation_input(input: ListWorksheetsInput) -> ListWorksheetsInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the Mingdao provider Query."
class _StartQuery(dex.Step[ListWorksheetsInput]):
    def get_step_type(self) -> str:
        return "StartListWorksheetsQuery"

    def execute(self, context: dex.Context, input: ListWorksheetsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListWorksheetsQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated Mingdao Query result."
class _CompleteQuery(dex.Step[QueryResult[ListWorksheetsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListWorksheetsQuery"

    def execute(self, context: dex.Context, input: QueryResult[ListWorksheetsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class MingdaoListWorksheetsFlow(dex.Flow[ListWorksheetsInput]):
    """Run the Mingdao Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Mingdao Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_list_worksheets_step(
            step_type="ListWorksheetsQuery",
            step_input_type=ListWorksheetsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Mingdao",
                "explanation": "Call Mingdao ListWorksheets using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "MingdaoListWorksheets"

    def get_steps(self) -> dex.StepList[ListWorksheetsInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
