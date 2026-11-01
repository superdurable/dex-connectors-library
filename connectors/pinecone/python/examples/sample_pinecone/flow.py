# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Pinecone sample Flow using the upsertVectors Mutation."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.pinecone import (
    Connection,
    UpsertVectorsInput,
    UpsertVectorsOutput,
    new_upsert_vectors_step,
)

FLOW_TYPE = "PineconeSample"
CONNECTION_NAME = "pinecone"
_CALL_STEP_TYPE = "CallPinecone"
_RESULT = dex.Attribute("pinecone-sample-result", UpsertVectorsOutput)


def map_to_operation_input(request: UpsertVectorsInput) -> UpsertVectorsInput:
    """Pass the validated Flow request to the connector."""
    return request


# dex:group group-id:pinecone-start group-label:"Pinecone sample"
# dex:explanation text:"Pass the start input to the connector operation."
class _StartRequest(dex.Step[UpsertVectorsInput]):
    def get_step_type(self) -> str:
        return "StartPineconeRequest"

    def execute(self, context: dex.Context, input: UpsertVectorsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_CALL_STEP_TYPE), input)


# dex:group group-id:pinecone-complete group-label:"Pinecone sample"
# dex:explanation text:"Complete the Flow with the provider response."
class _CompleteRequest(dex.Step[MutationResult[UpsertVectorsOutput]]):
    def get_step_type(self) -> str:
        return "CompletePineconeRequest"

    def execute(self, context: dex.Context, input: MutationResult[UpsertVectorsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SampleFlow(dex.Flow[UpsertVectorsInput]):
    """Run one upsertVectors write through the static pinecone connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the sample requires the static pinecone connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_upsert_vectors_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=UpsertVectorsInput,
            annotations={
                "groupId": "pinecone",
                "groupLabel": "Pinecone",
                "explanation": "Upsert vector IDs by absolute replacement in one namespace.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            upserted=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[UpsertVectorsInput]:
        """Register the start, provider write, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
