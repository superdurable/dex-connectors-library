# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one published WordPress post."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.wordpress import Connection, GetPostInput, Post, new_get_post_step

CONNECTION_NAME = "wordpress"
_OUTCOME = dex.Attribute("wordpress-python-published-post", Post)


def map_to_operation_input(request: GetPostInput) -> GetPostInput:
    """Forward the typed non-secret Flow input."""
    return request


# dex:group group-id:post-lookup group-label:"Post lookup"
# dex:explanation text:"Start the WordPress provider operation."
class _Start(dex.Step[GetPostInput]):
    def get_step_type(self) -> str:
        return "StartReadWordPressPost"

    def execute(self, context: dex.Context, input: GetPostInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ReadWordPressPost"), input)


# dex:group group-id:post-lookup group-label:"Post lookup"
# dex:explanation text:"Persist the confirmed result and complete the Flow."
class _Complete(dex.Step[QueryResult[Post]]):
    def get_step_type(self) -> str:
        return "CompleteWordPressPostLookup"

    def execute(self, context: dex.Context, input: QueryResult[Post]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class PostLookupFlow(dex.Flow[GetPostInput]):
    """Read one published WordPress post."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("PostLookupFlow requires the static wordpress connection")
        self._start = _Start()
        self._complete = _Complete()
        self._provider = new_get_post_step(
            step_type="ReadWordPressPost",
            step_input_type=GetPostInput,
            annotations={
                "groupId": "post-lookup",
                "groupLabel": "Post lookup",
                "explanation": "Read one published WordPress post.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable WordPress Flow identity."""
        return "WordPressPythonPostLookupFlow"

    def get_steps(self) -> dex.StepList[GetPostInput]:
        """Register start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._provider, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the provider result for this Flow."""
        return dex.PersistenceSchema.of(_OUTCOME)
