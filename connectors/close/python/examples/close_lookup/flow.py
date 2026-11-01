# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Close lookup Flow: list leads, then read the first lead."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.close import (
    Connection,
    GetLeadInput,
    Lead,
    LeadPage,
    ListLeadsInput,
    new_get_lead_step,
    new_list_leads_step,
)

FLOW_TYPE = "CloseLookup"
CONNECTION_NAME = "close"
_FIRST_STEP_TYPE = "ReadCloseFirst"
_SECOND_STEP_TYPE = "ReadCloseSecond"
_ANNOTATION_GROUP = {"groupId": "close-lookup", "groupLabel": "Close lookup"}


@dataclass(frozen=True, slots=True)
class CloseLookupRequest:
    """Number of leads to skip before the page whose first lead is read."""

    skip: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class CloseLookupOutcome:
    """The first lead on the page, or an empty lead when the page had none."""

    lead: Lead = field(default_factory=Lead)


def map_to_list_leads_input(request: CloseLookupRequest) -> ListLeadsInput:
    """Read a page of ten leads after the requested offset."""
    return ListLeadsInput(skip=request.skip, limit=10)


def map_to_get_lead_input(lead: GetLeadInput) -> GetLeadInput:
    """Read the lead chosen from the page unchanged."""
    return lead


# dex:group group-id:close-lookup group-label:"Close lookup"
# dex:explanation text:"Start the lookup with the requested lead offset."
class _StartRequest(dex.Step[CloseLookupRequest]):
    def get_step_type(self) -> str:
        return "StartCloseLookup"

    def execute(self, context: dex.Context, input: CloseLookupRequest) -> dex.StepDecision:
        if input.skip < 0:
            return dex.force_fail("skip must not be negative")
        return dex.go_to(dex.StepRef(_FIRST_STEP_TYPE), input)


# dex:group group-id:close-lookup group-label:"Close lookup"
# dex:explanation text:"Complete when the page is empty, otherwise read its first lead."
class _AfterFirst(dex.Step[QueryResult[LeadPage]]):
    def get_step_type(self) -> str:
        return "AfterCloseFirst"

    def execute(self, context: dex.Context, input: QueryResult[LeadPage]) -> dex.StepDecision:
        if not input.value.leads:
            return dex.graceful_complete(CloseLookupOutcome())
        return dex.go_to(dex.StepRef(_SECOND_STEP_TYPE), GetLeadInput(input.value.leads[0].id))


# dex:group group-id:close-lookup group-label:"Close lookup"
# dex:explanation text:"Complete with the lead Close returned."
class _CompleteLookup(dex.Step[QueryResult[Lead]]):
    def get_step_type(self) -> str:
        return "CompleteCloseLookup"

    def execute(self, context: dex.Context, input: QueryResult[Lead]) -> dex.StepDecision:
        return dex.graceful_complete(CloseLookupOutcome(input.value))


class CloseLookupFlow(dex.Flow[CloseLookupRequest]):
    """List Close leads and read the first one through the static close connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Close lookup requires the static close connection")
        self._start = _StartRequest()
        self._after_first = _AfterFirst()
        self._complete = _CompleteLookup()
        self._list = new_list_leads_step(
            step_type=_FIRST_STEP_TYPE,
            step_input_type=CloseLookupRequest,
            annotations={**_ANNOTATION_GROUP, "explanation": "Read Close records."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_list_leads_input,
            listed=self._after_first,
        )
        self._get = new_get_lead_step(
            step_type=_SECOND_STEP_TYPE,
            step_input_type=GetLeadInput,
            annotations={**_ANNOTATION_GROUP, "explanation": "Read the related Close resource."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_lead_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[CloseLookupRequest]:
        """Register the start, both Close reads, and their result Steps."""
        return dex.StepList.start_step(self._start).other_steps(
            self._list, self._after_first, self._get, self._complete
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist nothing; the outcome is the Flow's completion value."""
        return dex.PersistenceSchema.of()
