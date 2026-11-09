# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one page of published WordPress posts."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.wordpress import Connection, ListPostsInput, PostPage, new_list_posts_step

CONNECTION_NAME = "wordpress"
_OUTCOME = dex.Attribute("wordpress-python-post-page", PostPage)


def map_to_operation_input(request: ListPostsInput) -> ListPostsInput:
    """Forward the typed non-secret Flow input."""
    return request


# dex:group group-id:post-list group-label:"Post list"
# dex:explanation text:"Start the WordPress provider operation."
class _Start(dex.Step[ListPostsInput]):
    def get_step_type(self) -> str:
        return "StartListWordPressPosts"

    def execute(self, context: dex.Context, input: ListPostsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListWordPressPosts"), input)


# dex:group group-id:post-list group-label:"Post list"
# dex:explanation text:"Persist the confirmed result and complete the Flow."
class _Complete(dex.Step[QueryResult[PostPage]]):
    def get_step_type(self) -> str:
        return "CompleteWordPressPostList"

    def execute(self, context: dex.Context, input: QueryResult[PostPage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class PostListFlow(dex.Flow[ListPostsInput]):
    """Read one page of published WordPress posts."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("PostListFlow requires the static wordpress connection")
        self._start = _Start()
        self._complete = _Complete()
        self._provider = new_list_posts_step(
            step_type="ListWordPressPosts",
            step_input_type=ListPostsInput,
            annotations={
                "groupId": "post-list",
                "groupLabel": "Post list",
                "explanation": "Read one page of published WordPress posts.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable WordPress Flow identity."""
        return "WordPressPythonPostListFlow"

    def get_steps(self) -> dex.StepList[ListPostsInput]:
        """Register start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._provider, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the provider result for this Flow."""
        return dex.PersistenceSchema.of(_OUTCOME)
