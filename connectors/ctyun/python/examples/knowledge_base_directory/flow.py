# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List accessible CTYun knowledge-base metadata through a Dex Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.ctyun import (
    Connection,
    ListKnowledgeBasesInput,
    ListKnowledgeBasesOutput,
    new_list_knowledge_bases_step,
)

CONNECTION_NAME = "ctyun"
knowledge_bases_attribute = dex.Attribute("ctyun-knowledge-bases-python", ListKnowledgeBasesOutput)


# dex:group group-id:directory group-label:"Knowledge-base directory"
# dex:explanation text:"Record resource metadata and complete the Flow."
class RecordKnowledgeBases(dex.Step[QueryResult[ListKnowledgeBasesOutput]]):
    """Persist the non-secret resource summaries and complete the Flow."""

    def get_step_type(self) -> str:
        """Return the stable recording Step identity."""
        return "RecordCTYunKnowledgeBases"

    def execute(
        self, context: dex.Context, input: QueryResult[ListKnowledgeBasesOutput]
    ) -> dex.StepDecision:
        """Record the list of IDs, names, and file counts."""
        knowledge_bases_attribute.set(context, input.value)
        return dex.graceful_complete(input.value)


class KnowledgeBaseDirectoryFlow(dex.Flow[ListKnowledgeBasesInput]):
    """List the connected CTYun tenant's accessible knowledge bases."""

    def __init__(self, connection: Connection) -> None:
        """Bind the read-only provider Query and completion Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("directory Flow requires the static CTYun connection")
        self.record = RecordKnowledgeBases()
        self.read = new_list_knowledge_bases_step(
            step_type="ListCTYunKnowledgeBases",
            step_input_type=ListKnowledgeBasesInput,
            annotations={
                "groupId": "directory",
                "groupLabel": "Knowledge-base directory",
                "explanation": "Sign and GET the accessible knowledge-base metadata.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            listed=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the durable Flow identity."""
        return "CTYunKnowledgeBaseDirectoryPython"

    def get_steps(self) -> dex.StepList[ListKnowledgeBasesInput]:
        """Register the provider and recording Steps."""
        return dex.StepList.start_step(self.read).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the knowledge-base summary Attribute."""
        return dex.PersistenceSchema.of(knowledge_bases_attribute)
