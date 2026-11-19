# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable GuGuData text segmentation Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.gugudata import (
    Connection,
    SegmentTextInput,
    SegmentTextOutput,
    new_segment_text_step,
)

CONNECTION_NAME = "gugudata"
_RESULT = dex.Attribute("gugudata-text-segmentation", SegmentTextOutput)


def map_to_operation_input(request: SegmentTextInput) -> SegmentTextInput:
    """Forward the requested text segmentation."""
    return request


# dex:group group-id:gugudata group-label:"GuGuData"
# dex:explanation text:"Start one GuGuData text segmentation query."
class _StartSegmentation(dex.Step[SegmentTextInput]):
    def execute(self, context: dex.Context, input: SegmentTextInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGuGuData"), input)


# dex:group group-id:gugudata group-label:"GuGuData"
# dex:explanation text:"Persist segmented text before completion."
class _CompleteSegmentation(dex.Step[QueryResult[SegmentTextOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[SegmentTextOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SegmentationFlow(dex.Flow[SegmentTextInput]):
    """Segment one short text through the static GuGuData connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("SegmentationFlow requires the static GuGuData connection")
        self._start = _StartSegmentation()
        self._complete = _CompleteSegmentation()
        self._call = new_segment_text_step(
            step_type="CallGuGuData",
            step_input_type=SegmentTextInput,
            annotations={
                "groupId": "gugudata",
                "groupLabel": "GuGuData",
                "explanation": "Segment the requested text with GuGuData.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            segmented=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable text segmentation Flow identity."""
        return "GuGuDataSegmentation"

    def get_steps(self) -> dex.StepList[SegmentTextInput]:
        """Register the text segmentation Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider segmentation result only."""
        return dex.PersistenceSchema.of(_RESULT)
