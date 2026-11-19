# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Zilliz listClusters Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zilliz import (
    Connection,
    ListClustersInput,
    ListClustersOutput,
    new_list_clusters_step,
)

CONNECTION_NAME = "zilliz"
_RESULT = dex.Attribute("zilliz-result", ListClustersOutput)


def map_to_operation_input(request: ListClustersInput) -> ListClustersInput:
    """Forward the provider request."""
    return request


# dex:group group-id:zilliz group-label:"Zilliz"
# dex:explanation text:"Start the Zilliz query."
class _StartQuery(dex.Step[ListClustersInput]):
    def execute(self, context: dex.Context, input: ListClustersInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:zilliz group-label:"Zilliz"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListClustersOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListClustersOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListClustersFlow(dex.Flow[ListClustersInput]):
    """Run one Zilliz query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Zilliz Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_clusters_step(
            step_type="CallProvider",
            step_input_type=ListClustersInput,
            annotations={
                "groupId": "zilliz",
                "groupLabel": "Zilliz",
                "explanation": "Run Zilliz listClusters.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListClustersZilliz"

    def get_steps(self) -> dex.StepList[ListClustersInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
