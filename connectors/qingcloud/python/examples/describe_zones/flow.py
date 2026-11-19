# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Qingcloud response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.qingcloud import (
    Connection,
    DescribeZonesInput,
    ZonesPage,
    new_describe_zones_step,
)

CONNECTION_NAME = "qingcloud"
_RESULT = dex.Attribute("qingcloud-query-result", ZonesPage)


def map_to_operation_input(input: DescribeZonesInput) -> DescribeZonesInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the Qingcloud provider Query."
class _StartQuery(dex.Step[DescribeZonesInput]):
    def get_step_type(self) -> str:
        return "StartDescribeZonesQuery"

    def execute(self, context: dex.Context, input: DescribeZonesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("DescribeZonesQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated Qingcloud Query result."
class _CompleteQuery(dex.Step[QueryResult[ZonesPage]]):
    def get_step_type(self) -> str:
        return "CompleteDescribeZonesQuery"

    def execute(self, context: dex.Context, input: QueryResult[ZonesPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class QingcloudDescribeZonesFlow(dex.Flow[DescribeZonesInput]):
    """Run the Qingcloud Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Qingcloud Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_describe_zones_step(
            step_type="DescribeZonesQuery",
            step_input_type=DescribeZonesInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Qingcloud",
                "explanation": "Call Qingcloud DescribeZones using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "QingcloudDescribeZones"

    def get_steps(self) -> dex.StepList[DescribeZonesInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
