# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Greenhouse candidate inspection Flow using getCandidate."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.greenhouse import (
    CandidateProfile,
    Connection,
    GetCandidateInput,
    new_get_candidate_step,
)

FLOW_TYPE = "GreenhouseCandidateInspectionPython"
CONNECTION_NAME = "greenhouse"
_CALL_STEP_TYPE = "CallGreenhouse"
_RESULT = dex.Attribute("greenhouse-candidate", CandidateProfile)


def map_to_operation_input(request: GetCandidateInput) -> GetCandidateInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:greenhouse group-label:"Greenhouse"
# dex:explanation text:"Start the Greenhouse request."
class _StartRequest(dex.Step[GetCandidateInput]):
    def get_step_type(self) -> str:
        return "StartGreenhouseRequest"

    def execute(self, context: dex.Context, input: GetCandidateInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGreenhouse"), input)


# dex:group group-id:greenhouse group-label:"Greenhouse"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[CandidateProfile]]):
    def get_step_type(self) -> str:
        return "CompleteGreenhouseRequest"

    def execute(self, context: dex.Context, input: QueryResult[CandidateProfile]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CandidateInspectionFlow(dex.Flow[GetCandidateInput]):
    """Read one candidate through the static greenhouse connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("candidate inspection requires the static greenhouse connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_candidate_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetCandidateInput,
            annotations={
                "groupId": "greenhouse",
                "groupLabel": "Greenhouse",
                "explanation": "Read one candidate from the connected Greenhouse account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "GreenhouseCandidateInspectionPython"

    def get_steps(self) -> dex.StepList[GetCandidateInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
