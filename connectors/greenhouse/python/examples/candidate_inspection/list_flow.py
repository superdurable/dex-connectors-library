# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Greenhouse candidate pagination Flow using listCandidates."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.greenhouse import (
    CandidateProfilesPage,
    Connection,
    ListCandidatesInput,
    new_list_candidates_step,
)

from .flow import CONNECTION_NAME

FLOW_TYPE = "GreenhouseCandidateListPython"
_CALL_STEP_TYPE = "CallGreenhouseList"
_RESULT = dex.Attribute("greenhouse-candidate-page", CandidateProfilesPage)


def map_to_operation_input(request: ListCandidatesInput) -> ListCandidatesInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:greenhouse group-label:"Greenhouse"
# dex:explanation text:"Start the Greenhouse request."
class _StartRequest(dex.Step[ListCandidatesInput]):
    def get_step_type(self) -> str:
        return "StartGreenhouseList"

    def execute(self, context: dex.Context, input: ListCandidatesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGreenhouseList"), input)


# dex:group group-id:greenhouse group-label:"Greenhouse"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[CandidateProfilesPage]]):
    def get_step_type(self) -> str:
        return "CompleteGreenhouseListRequest"

    def execute(self, context: dex.Context, input: QueryResult[CandidateProfilesPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CandidateListFlow(dex.Flow[ListCandidatesInput]):
    """Read one page of candidate profiles through the static greenhouse connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("candidate pagination requires the static greenhouse connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_candidates_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListCandidatesInput,
            annotations={
                "groupId": "greenhouse",
                "groupLabel": "Greenhouse",
                "explanation": "Read one page of candidate profiles from the connected Greenhouse account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "GreenhouseCandidateListPython"

    def get_steps(self) -> dex.StepList[ListCandidatesInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
