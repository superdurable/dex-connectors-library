# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Harvest lookup Flow: list projects, then recent time entries."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.harvest import (
    Connection,
    ListProjectsInput,
    ListTimeEntriesInput,
    ProjectPage,
    TimeEntry,
    TimeEntryPage,
    new_list_projects_step,
    new_list_time_entries_step,
)

FLOW_TYPE = "HarvestLookup"
CONNECTION_NAME = "harvest"
_FIRST_STEP_TYPE = "ReadHarvestFirst"
_SECOND_STEP_TYPE = "ReadHarvestSecond"
_ANNOTATION_GROUP = {"groupId": "harvest-lookup", "groupLabel": "Harvest lookup"}


@dataclass(frozen=True, slots=True)
class HarvestLookupRequest:
    """Projects per page; zero uses Harvest's connector default of 100."""

    per_page: int = field(default=0, metadata={"dex_json_name": "perPage", "dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class HarvestLookupOutcome:
    """Time entries from the first page that followed the project read."""

    time_entries: tuple[TimeEntry, ...] = field(default=(), metadata={"dex_json_name": "timeEntries"})


def map_to_list_projects_input(request: HarvestLookupRequest) -> ListProjectsInput:
    """Read the first page of projects at the requested size."""
    return ListProjectsInput(per_page=request.per_page)


def map_to_list_time_entries_input(projects: QueryResult[ProjectPage]) -> ListTimeEntriesInput:
    """Read the ten most recent time entries after the project read."""
    return ListTimeEntriesInput(per_page=10)


# dex:group group-id:harvest-lookup group-label:"Harvest lookup"
# dex:explanation text:"Start the lookup with the requested page size."
class _StartRequest(dex.Step[HarvestLookupRequest]):
    def get_step_type(self) -> str:
        return "StartHarvestLookup"

    def execute(self, context: dex.Context, input: HarvestLookupRequest) -> dex.StepDecision:
        if not 0 <= input.per_page <= 2000:
            return dex.force_fail("perPage must be 0 to 2000")
        return dex.go_to(dex.StepRef(_FIRST_STEP_TYPE), input)


# dex:group group-id:harvest-lookup group-label:"Harvest lookup"
# dex:explanation text:"Continue from the project page to the time entry read."
class _AfterFirst(dex.Step[QueryResult[ProjectPage]]):
    def get_step_type(self) -> str:
        return "AfterHarvestFirst"

    def execute(self, context: dex.Context, input: QueryResult[ProjectPage]) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_SECOND_STEP_TYPE), input)


# dex:group group-id:harvest-lookup group-label:"Harvest lookup"
# dex:explanation text:"Complete with the time entries Harvest returned."
class _CompleteLookup(dex.Step[QueryResult[TimeEntryPage]]):
    def get_step_type(self) -> str:
        return "CompleteHarvestLookup"

    def execute(self, context: dex.Context, input: QueryResult[TimeEntryPage]) -> dex.StepDecision:
        return dex.graceful_complete(HarvestLookupOutcome(input.value.time_entries))


class HarvestLookupFlow(dex.Flow[HarvestLookupRequest]):
    """Read Harvest projects and then time entries through the static harvest connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Harvest lookup requires the static harvest connection")
        self._start = _StartRequest()
        self._after_first = _AfterFirst()
        self._complete = _CompleteLookup()
        self._projects = new_list_projects_step(
            step_type=_FIRST_STEP_TYPE,
            step_input_type=HarvestLookupRequest,
            annotations={**_ANNOTATION_GROUP, "explanation": "Read Harvest records."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_list_projects_input,
            listed=self._after_first,
        )
        self._time_entries = new_list_time_entries_step(
            step_type=_SECOND_STEP_TYPE,
            step_input_type=QueryResult[ProjectPage],
            annotations={**_ANNOTATION_GROUP, "explanation": "Read the related Harvest resource."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_list_time_entries_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[HarvestLookupRequest]:
        """Register the start, both Harvest reads, and their result Steps."""
        return dex.StepList.start_step(self._start).other_steps(
            self._projects, self._after_first, self._time_entries, self._complete
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist nothing; the outcome is the Flow's completion value."""
        return dex.PersistenceSchema.of()
