# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Reddit post Flow using the getPost Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.reddit import (
    Connection,
    GetPostInput,
    Post,
    new_get_post_step,
)

FLOW_TYPE = "RedditPostLookupPython"
CONNECTION_NAME = "reddit"
_CALL_STEP_TYPE = "CallGetPost"
_RESULT = dex.Attribute("reddit-python-post", Post)


def map_to_operation_input(request: GetPostInput) -> GetPostInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:reddit group-label:"Reddit"
# dex:explanation text:"Start the Reddit request."
class _StartRequest(dex.Step[GetPostInput]):
    def get_step_type(self) -> str:
        return "StartGetPost"

    def execute(self, context: dex.Context, input: GetPostInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetPost"), input)


# dex:group group-id:reddit group-label:"Reddit"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[Post]]):
    def get_step_type(self) -> str:
        return "CompleteGetPost"

    def execute(self, context: dex.Context, input: QueryResult[Post]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PostFlow(dex.Flow[GetPostInput]):
    """Run one getPost read through the static reddit connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the post lookup Flow requires the static reddit connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_post_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetPostInput,
            annotations={
                "groupId": "reddit",
                "groupLabel": "Reddit",
                "explanation": "Read the requested Reddit post.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "RedditPostLookupPython"

    def get_steps(self) -> dex.StepList[GetPostInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
