# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List Baichuan knowledge bases through one durable Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.baichuan import (
    Connection,
    ListKnowledgeBasesInput,
    ListKnowledgeBasesOutput,
    new_list_knowledge_bases_step,
)

CONNECTION_NAME = "baichuan"
_RESULT = dex.Attribute("baichuan-knowledge-base-list", ListKnowledgeBasesOutput)


def map_to_operation_input(input: ListKnowledgeBasesInput) -> ListKnowledgeBasesInput:
    """Forward the empty list request to the Baichuan Query."""
    return input


# dex:group group-id:knowledge-bases group-label:"Knowledge bases"
# dex:explanation text:"Start the Baichuan knowledge-base list query."
class _StartList(dex.Step[ListKnowledgeBasesInput]):
    def get_step_type(self) -> str:
        return "StartBaichuanKnowledgeBaseList"

    def execute(self, context: dex.Context, input: ListKnowledgeBasesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListBaichuanKnowledgeBases"), input)


# dex:group group-id:knowledge-bases group-label:"Knowledge bases"
# dex:explanation text:"Persist the returned Baichuan knowledge bases."
class _CompleteList(dex.Step[QueryResult[ListKnowledgeBasesOutput]]):
    def get_step_type(self) -> str:
        return "CompleteBaichuanKnowledgeBaseList"

    def execute(self, context: dex.Context, input: QueryResult[ListKnowledgeBasesOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class KnowledgeBaseListFlow(dex.Flow[ListKnowledgeBasesInput]):
    """Durably list knowledge bases through the static Baichuan connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("knowledge-base list requires the static Baichuan connection")
        self._start = _StartList()
        self._complete = _CompleteList()
        self._query = new_list_knowledge_bases_step(
            step_type="ListBaichuanKnowledgeBases",
            step_input_type=ListKnowledgeBasesInput,
            annotations={
                "groupId": "knowledge-bases",
                "groupLabel": "Knowledge bases",
                "explanation": "Read knowledge bases visible to the configured API key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable knowledge-base list Flow identity."""
        return "BaichuanKnowledgeBaseList"

    def get_steps(self) -> dex.StepList[ListKnowledgeBasesInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable knowledge-base list Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
