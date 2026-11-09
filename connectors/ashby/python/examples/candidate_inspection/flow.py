# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Ashby candidate inspection Flow using getCandidate."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.ashby import (
    CandidateProfile,
    Connection,
    GetCandidateInput,
    new_get_candidate_step,
)

FLOW_TYPE = "AshbyCandidateInspectionPython"
CONNECTION_NAME = "ashby"
_CALL_STEP_TYPE = "CallAshby"
_RESULT = dex.Attribute("ashby-candidate", CandidateProfile)


def map_to_operation_input(request: GetCandidateInput) -> GetCandidateInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:ashby group-label:"Ashby"
# dex:explanation text:"Start the Ashby request."
class _StartRequest(dex.Step[GetCandidateInput]):
    def get_step_type(self) -> str:
        return "StartAshbyRequest"

    def execute(self, context: dex.Context, input: GetCandidateInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallAshby"), input)


# dex:group group-id:ashby group-label:"Ashby"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[CandidateProfile]]):
    def get_step_type(self) -> str:
        return "CompleteAshbyRequest"

    def execute(self, context: dex.Context, input: QueryResult[CandidateProfile]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CandidateInspectionFlow(dex.Flow[GetCandidateInput]):
    """Read one candidate through the static ashby connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("candidate inspection requires the static ashby connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_candidate_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetCandidateInput,
            annotations={
                "groupId": "ashby",
                "groupLabel": "Ashby",
                "explanation": "Read one candidate from the connected Ashby account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AshbyCandidateInspectionPython"

    def get_steps(self) -> dex.StepList[GetCandidateInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
