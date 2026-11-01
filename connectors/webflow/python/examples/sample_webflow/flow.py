# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Webflow sample Flow using the listCollectionItems Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.webflow import (
    Connection,
    ListCollectionItemsInput,
    ListCollectionItemsOutput,
    new_list_collection_items_step,
)

FLOW_TYPE = "WebflowSample"
CONNECTION_NAME = "webflow"
_CALL_STEP_TYPE = "CallWebflow"
_RESULT = dex.Attribute("webflow-sample-result", ListCollectionItemsOutput)


def map_to_operation_input(request: ListCollectionItemsInput) -> ListCollectionItemsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:webflow group-label:"Webflow"
# dex:explanation text:"Start the Webflow request."
class _StartRequest(dex.Step[ListCollectionItemsInput]):
    def get_step_type(self) -> str:
        return "StartWebflowRequest"

    def execute(self, context: dex.Context, input: ListCollectionItemsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_CALL_STEP_TYPE), input)


# dex:group group-id:webflow group-label:"Webflow"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ListCollectionItemsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteWebflowRequest"

    def execute(
        self, context: dex.Context, input: QueryResult[ListCollectionItemsOutput]
    ) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SampleFlow(dex.Flow[ListCollectionItemsInput]):
    """Run one listCollectionItems read through the static webflow connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the sample requires the static webflow connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_collection_items_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListCollectionItemsInput,
            annotations={
                "groupId": "webflow",
                "groupLabel": "Webflow",
                "explanation": "List a page of staged CMS collection items.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ListCollectionItemsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
