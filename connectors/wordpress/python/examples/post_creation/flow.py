# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Create one WordPress draft or published post."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.wordpress import Connection, CreatedPost, CreatePostInput, new_create_post_step

CONNECTION_NAME = "wordpress"
_OUTCOME = dex.Attribute("wordpress-python-created-post", CreatedPost)


def map_to_operation_input(request: CreatePostInput) -> CreatePostInput:
    """Forward the typed non-secret Flow input."""
    return request


# dex:group group-id:post-creation group-label:"Post creation"
# dex:explanation text:"Start the WordPress provider operation."
class _Start(dex.Step[CreatePostInput]):
    def get_step_type(self) -> str:
        return "StartCreateWordPressPost"

    def execute(self, context: dex.Context, input: CreatePostInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CreateWordPressPost"), input)


# dex:group group-id:post-creation group-label:"Post creation"
# dex:explanation text:"Persist the confirmed result and complete the Flow."
class _Complete(dex.Step[MutationResult[CreatedPost]]):
    def get_step_type(self) -> str:
        return "CompleteWordPressPost"

    def execute(self, context: dex.Context, input: MutationResult[CreatedPost]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class PostCreationFlow(dex.Flow[CreatePostInput]):
    """Create one WordPress draft or published post."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("PostCreationFlow requires the static wordpress connection")
        self._start = _Start()
        self._complete = _Complete()
        self._provider = new_create_post_step(
            step_type="CreateWordPressPost",
            step_input_type=CreatePostInput,
            annotations={
                "groupId": "post-creation",
                "groupLabel": "Post creation",
                "explanation": "Create one WordPress draft or published post.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            created=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable WordPress Flow identity."""
        return "WordPressPythonPostCreationFlow"

    def get_steps(self) -> dex.StepList[CreatePostInput]:
        """Register start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._provider, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the provider result for this Flow."""
        return dex.PersistenceSchema.of(_OUTCOME)
