# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable TiankeChat listKnowledgeEntries Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.tiankechat import (
    Connection,
    ListKnowledgeEntriesInput,
    ListKnowledgeEntriesOutput,
    new_list_knowledge_entries_step,
)

CONNECTION_NAME = "tiankechat"
_RESULT = dex.Attribute("tiankechat-result", ListKnowledgeEntriesOutput)


def map_to_operation_input(request: ListKnowledgeEntriesInput) -> ListKnowledgeEntriesInput:
    """Forward the provider request."""
    return request


# dex:group group-id:tiankechat group-label:"TiankeChat"
# dex:explanation text:"Start the TiankeChat query."
class _StartQuery(dex.Step[ListKnowledgeEntriesInput]):
    def execute(self, context: dex.Context, input: ListKnowledgeEntriesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:tiankechat group-label:"TiankeChat"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListKnowledgeEntriesOutput]]):
    def execute(
        self, context: dex.Context, input: QueryResult[ListKnowledgeEntriesOutput]
    ) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListKnowledgeEntriesFlow(dex.Flow[ListKnowledgeEntriesInput]):
    """Run one TiankeChat query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("TiankeChat Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_knowledge_entries_step(
            step_type="CallProvider",
            step_input_type=ListKnowledgeEntriesInput,
            annotations={
                "groupId": "tiankechat",
                "groupLabel": "TiankeChat",
                "explanation": "Run TiankeChat listKnowledgeEntries.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListKnowledgeEntriesTiankeChat"

    def get_steps(self) -> dex.StepList[ListKnowledgeEntriesInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
