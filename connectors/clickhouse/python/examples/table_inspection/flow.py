# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable ClickHouse table inspection Flow using listTables."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.clickhouse import (
    Connection,
    ListTablesInput,
    TablesPage,
    new_list_tables_step,
)

FLOW_TYPE = "ClickHouseTableInspectionPython"
CONNECTION_NAME = "clickhouse"
_CALL_STEP_TYPE = "CallClickHouse"
_RESULT = dex.Attribute("clickhouse-tables", TablesPage)


def map_to_operation_input(request: ListTablesInput) -> ListTablesInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:clickhouse group-label:"ClickHouse"
# dex:explanation text:"Start the ClickHouse request."
class _StartRequest(dex.Step[ListTablesInput]):
    def get_step_type(self) -> str:
        return "StartClickHouseRequest"

    def execute(self, context: dex.Context, input: ListTablesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallClickHouse"), input)


# dex:group group-id:clickhouse group-label:"ClickHouse"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[TablesPage]]):
    def get_step_type(self) -> str:
        return "CompleteClickHouseRequest"

    def execute(self, context: dex.Context, input: QueryResult[TablesPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TableInspectionFlow(dex.Flow[ListTablesInput]):
    """Read table names through the static clickhouse connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("table inspection requires the static clickhouse connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_tables_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListTablesInput,
            annotations={
                "groupId": "clickhouse",
                "groupLabel": "ClickHouse",
                "explanation": "Read table names from the connected ClickHouse account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "ClickHouseTableInspectionPython"

    def get_steps(self) -> dex.StepList[ListTablesInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
