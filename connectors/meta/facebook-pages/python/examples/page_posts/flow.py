# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Facebook Pages post feed Flow using the listPosts Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.meta_facebook_pages import (
    Connection,
    ListPostsInput,
    PagePostsPage,
    new_list_posts_step,
)

FLOW_TYPE = "FacebookPagesPagePostsPython"
CONNECTION_NAME = "facebook-pages"
_CALL_STEP_TYPE = "CallListPosts"
_RESULT = dex.Attribute("facebook-pages-python-posts-page", PagePostsPage)


def map_to_operation_input(request: ListPostsInput) -> ListPostsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:facebook-pages group-label:"Facebook Pages"
# dex:explanation text:"Start the Facebook Pages request."
class _StartRequest(dex.Step[ListPostsInput]):
    def get_step_type(self) -> str:
        return "StartListPosts"

    def execute(self, context: dex.Context, input: ListPostsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListPosts"), input)


# dex:group group-id:facebook-pages group-label:"Facebook Pages"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[PagePostsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListPosts"

    def execute(self, context: dex.Context, input: QueryResult[PagePostsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PagePostsFlow(dex.Flow[ListPostsInput]):
    """Run one listPosts read through the static facebook-pages connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the Page post Flow requires the static facebook-pages connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_posts_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListPostsInput,
            annotations={
                "groupId": "facebook-pages",
                "groupLabel": "Facebook Pages",
                "explanation": "List Page post IDs and messages using the Page token.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "FacebookPagesPagePostsPython"

    def get_steps(self) -> dex.StepList[ListPostsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
