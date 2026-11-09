# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable X post inspection Flow using getPost."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.x_twitter import (
    Connection,
    GetPostInput,
    PostMetadata,
    new_get_post_step,
)

FLOW_TYPE = "XPostInspectionPython"
CONNECTION_NAME = "x-twitter"
_CALL_STEP_TYPE = "CallX"
_RESULT = dex.Attribute("x-post", PostMetadata)


def map_to_operation_input(request: GetPostInput) -> GetPostInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:x group-label:"X"
# dex:explanation text:"Start the X request."
class _StartRequest(dex.Step[GetPostInput]):
    def get_step_type(self) -> str:
        return "StartXRequest"

    def execute(self, context: dex.Context, input: GetPostInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallX"), input)


# dex:group group-id:x group-label:"X"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[PostMetadata]]):
    def get_step_type(self) -> str:
        return "CompleteXRequest"

    def execute(self, context: dex.Context, input: QueryResult[PostMetadata]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PostInspectionFlow(dex.Flow[GetPostInput]):
    """Read one post through the static x connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("post inspection requires the static x connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_post_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetPostInput,
            annotations={
                "groupId": "x",
                "groupLabel": "X",
                "explanation": "Read one post from the connected X account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "XPostInspectionPython"

    def get_steps(self) -> dex.StepList[GetPostInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
