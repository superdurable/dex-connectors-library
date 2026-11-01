# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Smartsheet lookup Flow: list sheets, then read the first sheet."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.smartsheet import (
    Connection,
    GetSheetInput,
    ListSheetsInput,
    Sheet,
    SheetPage,
    new_get_sheet_step,
    new_list_sheets_step,
)

FLOW_TYPE = "SmartsheetLookup"
CONNECTION_NAME = "smartsheet"
_FIRST_STEP_TYPE = "ReadSmartsheetFirst"
_SECOND_STEP_TYPE = "ReadSmartsheetSecond"
_ANNOTATION_GROUP = {"groupId": "smartsheet-lookup", "groupLabel": "Smartsheet lookup"}


@dataclass(frozen=True, slots=True)
class SmartsheetLookupRequest:
    """Sheet page whose first sheet is read; zero means the first page."""

    page: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class SmartsheetLookupOutcome:
    """The first sheet on the page, or an empty sheet when the page had none."""

    sheet: Sheet = field(default_factory=Sheet)


def map_to_list_sheets_input(request: SmartsheetLookupRequest) -> ListSheetsInput:
    """Read a page of ten sheets."""
    return ListSheetsInput(page=request.page, page_size=10)


def map_to_get_sheet_input(sheet: GetSheetInput) -> GetSheetInput:
    """Read the sheet chosen from the page unchanged."""
    return sheet


# dex:group group-id:smartsheet-lookup group-label:"Smartsheet lookup"
# dex:explanation text:"Start the lookup with the requested sheet page."
class _StartRequest(dex.Step[SmartsheetLookupRequest]):
    def get_step_type(self) -> str:
        return "StartSmartsheetLookup"

    def execute(self, context: dex.Context, input: SmartsheetLookupRequest) -> dex.StepDecision:
        if input.page < 0:
            return dex.force_fail("page must not be negative")
        return dex.go_to(dex.StepRef(_FIRST_STEP_TYPE), input)


# dex:group group-id:smartsheet-lookup group-label:"Smartsheet lookup"
# dex:explanation text:"Complete when the page is empty, otherwise read its first sheet."
class _AfterFirst(dex.Step[QueryResult[SheetPage]]):
    def get_step_type(self) -> str:
        return "AfterSmartsheetFirst"

    def execute(self, context: dex.Context, input: QueryResult[SheetPage]) -> dex.StepDecision:
        if not input.value.sheets:
            return dex.graceful_complete(SmartsheetLookupOutcome())
        return dex.go_to(dex.StepRef(_SECOND_STEP_TYPE), GetSheetInput(input.value.sheets[0].id))


# dex:group group-id:smartsheet-lookup group-label:"Smartsheet lookup"
# dex:explanation text:"Complete with the sheet Smartsheet returned."
class _CompleteLookup(dex.Step[QueryResult[Sheet]]):
    def get_step_type(self) -> str:
        return "CompleteSmartsheetLookup"

    def execute(self, context: dex.Context, input: QueryResult[Sheet]) -> dex.StepDecision:
        return dex.graceful_complete(SmartsheetLookupOutcome(input.value))


class SmartsheetLookupFlow(dex.Flow[SmartsheetLookupRequest]):
    """List Smartsheet sheets and read the first one through the static smartsheet connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Smartsheet lookup requires the static smartsheet connection")
        self._start = _StartRequest()
        self._after_first = _AfterFirst()
        self._complete = _CompleteLookup()
        self._list = new_list_sheets_step(
            step_type=_FIRST_STEP_TYPE,
            step_input_type=SmartsheetLookupRequest,
            annotations={**_ANNOTATION_GROUP, "explanation": "Read Smartsheet records."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_list_sheets_input,
            listed=self._after_first,
        )
        self._get = new_get_sheet_step(
            step_type=_SECOND_STEP_TYPE,
            step_input_type=GetSheetInput,
            annotations={**_ANNOTATION_GROUP, "explanation": "Read the related Smartsheet resource."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_sheet_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[SmartsheetLookupRequest]:
        """Register the start, both Smartsheet reads, and their result Steps."""
        return dex.StepList.start_step(self._start).other_steps(
            self._list, self._after_first, self._get, self._complete
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist nothing; the outcome is the Flow's completion value."""
        return dex.PersistenceSchema.of()
