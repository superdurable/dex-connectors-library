# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Shortcut story inspection Flow using getStory."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.shortcut import (
    Connection,
    GetStoryInput,
    StoryMetadata,
    new_get_story_step,
)

FLOW_TYPE = "ShortcutStoryInspectionPython"
CONNECTION_NAME = "shortcut"
_CALL_STEP_TYPE = "CallShortcut"
_RESULT = dex.Attribute("shortcut-story", StoryMetadata)


def map_to_operation_input(request: GetStoryInput) -> GetStoryInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:shortcut group-label:"Shortcut"
# dex:explanation text:"Start the Shortcut request."
class _StartRequest(dex.Step[GetStoryInput]):
    def get_step_type(self) -> str:
        return "StartShortcutRequest"

    def execute(self, context: dex.Context, input: GetStoryInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallShortcut"), input)


# dex:group group-id:shortcut group-label:"Shortcut"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[StoryMetadata]]):
    def get_step_type(self) -> str:
        return "CompleteShortcutRequest"

    def execute(self, context: dex.Context, input: QueryResult[StoryMetadata]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class StoryInspectionFlow(dex.Flow[GetStoryInput]):
    """Read one story through the static shortcut connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("story inspection requires the static shortcut connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_story_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetStoryInput,
            annotations={
                "groupId": "shortcut",
                "groupLabel": "Shortcut",
                "explanation": "Read one story from the connected Shortcut account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "ShortcutStoryInspectionPython"

    def get_steps(self) -> dex.StepList[GetStoryInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
