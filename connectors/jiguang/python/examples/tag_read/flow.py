# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Jiguang provider result in a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.jiguang import (
    ApplicationTags,
    Connection,
    ListApplicationTagsInput,
    new_list_application_tags_step,
)

CONNECTION_NAME = "jiguang"
_RESULT = dex.Attribute("jiguang-application-tags", ApplicationTags)


def map_to_operation_input(input: ListApplicationTagsInput) -> ListApplicationTagsInput:
    """Pass the public Flow input to the provider Query."""
    return input


# dex:group group-id:jiguang group-label:"Jiguang"
# dex:explanation text:"Start the Jiguang Query for this Flow."
class _Start(dex.Step[ListApplicationTagsInput]):
    def get_step_type(self) -> str:
        return "StartApplicationTagsFlow"

    def execute(self, context: dex.Context, input: ListApplicationTagsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListApplicationTagsQuery"), input)


# dex:group group-id:jiguang group-label:"Jiguang"
# dex:explanation text:"Persist the validated provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[ApplicationTags]]):
    def get_step_type(self) -> str:
        return "CompleteApplicationTags"

    def execute(self, context: dex.Context, input: QueryResult[ApplicationTags]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ApplicationTagsFlow(dex.Flow[ListApplicationTagsInput]):
    """Call Jiguang through the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Jiguang connection name differs")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_list_application_tags_step(
            step_type="ListApplicationTagsQuery",
            step_input_type=ListApplicationTagsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Jiguang",
                "explanation": "Read tags for the configured JPush application.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable application Flow identity."""
        return "JiguangApplicationTagsFlow"

    def get_steps(self) -> dex.StepList[ListApplicationTagsInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
