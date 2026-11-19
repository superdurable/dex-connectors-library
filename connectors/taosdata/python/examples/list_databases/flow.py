# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable TDengine Cloud listDatabases Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.taosdata import (
    Connection,
    ListDatabasesInput,
    ListDatabasesOutput,
    new_list_databases_step,
)

CONNECTION_NAME = "taosdata"
_RESULT = dex.Attribute("taosdata-result", ListDatabasesOutput)


def map_to_operation_input(request: ListDatabasesInput) -> ListDatabasesInput:
    """Forward the provider request."""
    return request


# dex:group group-id:taosdata group-label:"TDengine Cloud"
# dex:explanation text:"Start the TDengine Cloud query."
class _StartQuery(dex.Step[ListDatabasesInput]):
    def execute(self, context: dex.Context, input: ListDatabasesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:taosdata group-label:"TDengine Cloud"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListDatabasesOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListDatabasesOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListDatabasesFlow(dex.Flow[ListDatabasesInput]):
    """Run one TDengine Cloud query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("TDengine Cloud Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_databases_step(
            step_type="CallProvider",
            step_input_type=ListDatabasesInput,
            annotations={
                "groupId": "taosdata",
                "groupLabel": "TDengine Cloud",
                "explanation": "Run TDengine Cloud listDatabases.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListDatabasesTaosdata"

    def get_steps(self) -> dex.StepList[ListDatabasesInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
