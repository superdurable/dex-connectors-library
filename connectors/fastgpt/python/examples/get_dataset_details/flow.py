# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one knowledge base through a statically named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.fastgpt import (
    Connection,
    GetDatasetDetailsInput,
    GetDatasetDetailsOutput,
    new_get_dataset_details_step,
)

CONNECTION_NAME = "fastgpt"
_OUTCOME = dex.Attribute("fastgpt-get-dataset-details-outcome", GetDatasetDetailsOutput)


def map_to_operation_input(input: GetDatasetDetailsInput) -> GetDatasetDetailsInput:
    """Pass the knowledge base ID to the provider Query."""
    return input


# dex:group group-id:get-dataset-details group-label:"Get dataset details"
# dex:explanation text:"Start the FastGPT knowledge base metadata read."
class _StartRequest(dex.Step[GetDatasetDetailsInput]):
    def get_step_type(self) -> str:
        return "StartGetDatasetDetails"

    def execute(self, context: dex.Context, input: GetDatasetDetailsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetDatasetDetails"), input)


# dex:group group-id:get-dataset-details group-label:"Get dataset details"
# dex:explanation text:"Persist dataset details, then complete the Flow."
class _CompleteResult(dex.Step[QueryResult[GetDatasetDetailsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetDatasetDetails"

    def execute(self, context: dex.Context, input: QueryResult[GetDatasetDetailsOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetDatasetDetailsFlow(dex.Flow[GetDatasetDetailsInput]):
    """Read a knowledge base from the static `fastgpt` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("dataset details query requires the static fastgpt connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_get_dataset_details_step(
            step_type="CallGetDatasetDetails",
            step_input_type=GetDatasetDetailsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "FastGPT",
                "explanation": "Call FastGPT's getDatasetDetails operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable type shown in Dex Web."""
        return "FastGPTGetDatasetDetails"

    def get_steps(self) -> dex.StepList[GetDatasetDetailsInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the returned knowledge base metadata."""
        return dex.PersistenceSchema.of(_OUTCOME)
