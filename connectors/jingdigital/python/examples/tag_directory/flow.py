# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable JINGdigital tag-page Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.jingdigital import (
    Connection,
    ListTagsInput,
    ListTagsOutput,
    new_list_tags_step,
)

CONNECTION_NAME = "jingdigital"
_RESULT = dex.Attribute("jingdigital-tag-page", ListTagsOutput)


def map_to_operation_input(request: ListTagsInput) -> ListTagsInput:
    """Forward the requested tag page."""
    return request


# dex:group group-id:jingdigital group-label:"JINGdigital"
# dex:explanation text:"Start one JINGdigital tag-page query."
class _StartTagDirectory(dex.Step[ListTagsInput]):
    def execute(self, context: dex.Context, input: ListTagsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallJINGdigital"), input)


# dex:group group-id:jingdigital group-label:"JINGdigital"
# dex:explanation text:"Persist tag identifiers and pagination before completion."
class _CompleteTagDirectory(dex.Step[QueryResult[ListTagsOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListTagsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TagDirectoryFlow(dex.Flow[ListTagsInput]):
    """List one tag page through the static JINGdigital connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("TagDirectoryFlow requires the static JINGdigital connection")
        self._start = _StartTagDirectory()
        self._complete = _CompleteTagDirectory()
        self._call = new_list_tags_step(
            step_type="CallJINGdigital",
            step_input_type=ListTagsInput,
            annotations={
                "groupId": "jingdigital",
                "groupLabel": "JINGdigital",
                "explanation": "Read ordinary marketing tag IDs, names, levels, and pagination.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable tag-page Flow identity."""
        return "JINGdigitalTagDirectory"

    def get_steps(self) -> dex.StepList[ListTagsInput]:
        """Register the tag-page Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist tag IDs, names, and pagination only."""
        return dex.PersistenceSchema.of(_RESULT)
