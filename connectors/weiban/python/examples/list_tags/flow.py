# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Weiban listTags Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.weiban import (
    Connection,
    ListTagsInput,
    TagsResult,
    new_list_tags_step,
)

CONNECTION_NAME = "weiban"
_RESULT = dex.Attribute("weiban-result", TagsResult)


def map_to_operation_input(request: ListTagsInput) -> ListTagsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:weiban group-label:"Weiban"
# dex:explanation text:"Start the Weiban query."
class _StartQuery(dex.Step[ListTagsInput]):
    def execute(self, context: dex.Context, input: ListTagsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:weiban group-label:"Weiban"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[TagsResult]]):
    def execute(self, context: dex.Context, input: QueryResult[TagsResult]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListTagsFlow(dex.Flow[ListTagsInput]):
    """Run one Weiban query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Weiban Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_tags_step(
            step_type="CallProvider",
            step_input_type=ListTagsInput,
            annotations={
                "groupId": "weiban",
                "groupLabel": "Weiban",
                "explanation": "Run Weiban listTags.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListTagsWeiban"

    def get_steps(self) -> dex.StepList[ListTagsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
