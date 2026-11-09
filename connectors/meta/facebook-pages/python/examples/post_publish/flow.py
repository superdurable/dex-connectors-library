# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Facebook Pages text publishing Flow."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.meta_facebook_pages import (
    Connection,
    PublishedPost,
    PublishTextPostInput,
    new_publish_text_post_step,
)

FLOW_TYPE = "FacebookPagesPostPublishPython"
CONNECTION_NAME = "facebook-pages"
_CALL_STEP_TYPE = "CallPublishTextPost"
_RESULT = dex.Attribute("facebook-pages-python-published-post", PublishedPost)


def map_to_operation_input(request: PublishTextPostInput) -> PublishTextPostInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:facebook-pages group-label:"Facebook Pages"
# dex:explanation text:"Start the Facebook Pages request."
class _StartRequest(dex.Step[PublishTextPostInput]):
    def get_step_type(self) -> str:
        return "StartPublishTextPost"

    def execute(self, context: dex.Context, input: PublishTextPostInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallPublishTextPost"), input)


# dex:group group-id:facebook-pages group-label:"Facebook Pages"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[MutationResult[PublishedPost]]):
    def get_step_type(self) -> str:
        return "CompletePublishTextPost"

    def execute(self, context: dex.Context, input: MutationResult[PublishedPost]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PostPublishFlow(dex.Flow[PublishTextPostInput]):
    """Publish one text post through the static facebook-pages connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the Page publishing Flow requires the static facebook-pages connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_publish_text_post_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=PublishTextPostInput,
            annotations={
                "groupId": "facebook-pages",
                "groupLabel": "Facebook Pages",
                "explanation": "Publish one plain-text post using the Page token.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            published=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "FacebookPagesPostPublishPython"

    def get_steps(self) -> dex.StepList[PublishTextPostInput]:
        """Register the start, provider publish, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
