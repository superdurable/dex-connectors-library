# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Segment source inspection Flow using getSource."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.twilio_segment import (
    Connection,
    GetSourceInput,
    SourceProfile,
    new_get_source_step,
)

FLOW_TYPE = "SegmentSourceInspectionPython"
CONNECTION_NAME = "segment"
_CALL_STEP_TYPE = "CallSegment"
_RESULT = dex.Attribute("segment-source", SourceProfile)


def map_to_operation_input(request: GetSourceInput) -> GetSourceInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:segment group-label:"Segment"
# dex:explanation text:"Start the Segment request."
class _StartRequest(dex.Step[GetSourceInput]):
    def get_step_type(self) -> str:
        return "StartSegmentRequest"

    def execute(self, context: dex.Context, input: GetSourceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallSegment"), input)


# dex:group group-id:segment group-label:"Segment"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[SourceProfile]]):
    def get_step_type(self) -> str:
        return "CompleteSegmentRequest"

    def execute(self, context: dex.Context, input: QueryResult[SourceProfile]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SourceInspectionFlow(dex.Flow[GetSourceInput]):
    """Read one source through the static segment connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("source inspection requires the static segment connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_source_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetSourceInput,
            annotations={
                "groupId": "segment",
                "groupLabel": "Segment",
                "explanation": "Read one source from the connected Segment account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "SegmentSourceInspectionPython"

    def get_steps(self) -> dex.StepList[GetSourceInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
