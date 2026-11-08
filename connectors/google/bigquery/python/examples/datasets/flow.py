# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable BigQuery page Flow using the listDatasets Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_bigquery import (
    Connection,
    DatasetsPage,
    ListDatasetsInput,
    new_list_datasets_step,
)

FLOW_TYPE = "BigQueryDatasetsListPython"
CONNECTION_NAME = "google-bigquery"
_CALL_STEP_TYPE = "CallListDatasets"
_RESULT = dex.Attribute("google_bigquery-python-page", DatasetsPage)


def map_to_operation_input(request: ListDatasetsInput) -> ListDatasetsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:google-bigquery group-label:"BigQuery"
# dex:explanation text:"Start the BigQuery request."
class _StartRequest(dex.Step[ListDatasetsInput]):
    def get_step_type(self) -> str:
        return "StartListDatasets"

    def execute(self, context: dex.Context, input: ListDatasetsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListDatasets"), input)


# dex:group group-id:google-bigquery group-label:"BigQuery"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[DatasetsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListDatasets"

    def execute(self, context: dex.Context, input: QueryResult[DatasetsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DatasetsFlow(dex.Flow[ListDatasetsInput]):
    """Run one listDatasets read through the static google_bigquery connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the datasets lookup Flow requires the static google_bigquery connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_datasets_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListDatasetsInput,
            annotations={
                "groupId": "google-bigquery",
                "groupLabel": "BigQuery",
                "explanation": "Read one BigQuery page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "BigQueryDatasetsListPython"

    def get_steps(self) -> dex.StepList[ListDatasetsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
