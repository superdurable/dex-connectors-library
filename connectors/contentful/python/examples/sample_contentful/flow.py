# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Contentful sample Flow using the getEntry Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.contentful import Connection, GetEntryInput, GetEntryOutput, new_get_entry_step

FLOW_TYPE = "ContentfulSample"
CONNECTION_NAME = "contentful"
_CALL_STEP_TYPE = "CallContentful"
_RESULT = dex.Attribute("contentful-sample-result", GetEntryOutput)


def map_to_operation_input(request: GetEntryInput) -> GetEntryInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:contentful group-label:"Contentful"
# dex:explanation text:"Start the Contentful request."
class _StartRequest(dex.Step[GetEntryInput]):
    def get_step_type(self) -> str:
        return "StartContentfulRequest"

    def execute(self, context: dex.Context, input: GetEntryInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_CALL_STEP_TYPE), input)


# dex:group group-id:contentful group-label:"Contentful"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[GetEntryOutput]]):
    def get_step_type(self) -> str:
        return "CompleteContentfulRequest"

    def execute(self, context: dex.Context, input: QueryResult[GetEntryOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SampleFlow(dex.Flow[GetEntryInput]):
    """Run one getEntry read through the static contentful connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the sample requires the static contentful connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_entry_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetEntryInput,
            annotations={
                "groupId": "contentful",
                "groupLabel": "Contentful",
                "explanation": "Get one published Contentful entry.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetEntryInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
