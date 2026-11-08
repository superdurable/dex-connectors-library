# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Instagram Business media listing Flow using the listMedia Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.meta_instagram_business import (
    Connection,
    ListMediaInput,
    MediaListPage,
    new_list_media_step,
)

FLOW_TYPE = "InstagramBusinessMediaListPython"
CONNECTION_NAME = "instagram-business"
_CALL_STEP_TYPE = "CallListMedia"
_RESULT = dex.Attribute("instagram-business-python-media-page", MediaListPage)


def map_to_operation_input(request: ListMediaInput) -> ListMediaInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:instagram-business group-label:"Instagram Business"
# dex:explanation text:"Start the Instagram Business request."
class _StartRequest(dex.Step[ListMediaInput]):
    def get_step_type(self) -> str:
        return "StartListMedia"

    def execute(self, context: dex.Context, input: ListMediaInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListMedia"), input)


# dex:group group-id:instagram-business group-label:"Instagram Business"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[MediaListPage]]):
    def get_step_type(self) -> str:
        return "CompleteListMedia"

    def execute(self, context: dex.Context, input: QueryResult[MediaListPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class MediaListFlow(dex.Flow[ListMediaInput]):
    """Run one listMedia read through the static instagram-business connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the media listing Flow requires the static instagram-business connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_media_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListMediaInput,
            annotations={
                "groupId": "instagram-business",
                "groupLabel": "Instagram Business",
                "explanation": "List media IDs and captions using the User token.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "InstagramBusinessMediaListPython"

    def get_steps(self) -> dex.StepList[ListMediaInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
