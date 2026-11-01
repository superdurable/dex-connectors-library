# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Figma lookup Flow: read a file, then its comments."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.figma import (
    Connection,
    File,
    FileComment,
    FileComments,
    GetFileCommentsInput,
    GetFileInput,
    new_get_file_comments_step,
    new_get_file_step,
)

FLOW_TYPE = "FigmaLookup"
CONNECTION_NAME = "figma"
_FIRST_STEP_TYPE = "ReadFigmaFirst"
_SECOND_STEP_TYPE = "ReadFigmaSecond"
_ANNOTATION_GROUP = {"groupId": "figma-lookup", "groupLabel": "Figma lookup"}
_FILE_KEY = dex.Attribute("figma-file-key", str)


@dataclass(frozen=True, slots=True)
class FigmaLookupRequest:
    """Figma file key to read."""

    file_key: str = field(metadata={"dex_json_name": "fileKey"})


@dataclass(frozen=True, slots=True)
class FigmaLookupOutcome:
    """Comments on the file."""

    comments: tuple[FileComment, ...] = ()


def map_to_get_file_input(request: FigmaLookupRequest) -> GetFileInput:
    """Read the requested file."""
    return GetFileInput(request.file_key)


def map_to_get_file_comments_input(input: GetFileCommentsInput) -> GetFileCommentsInput:
    """Read the comments of the file chosen after the file read."""
    return input


# dex:group group-id:figma-lookup group-label:"Figma lookup"
# dex:explanation text:"Record the file key and start the lookup."
class _StartRequest(dex.Step[FigmaLookupRequest]):
    def get_step_type(self) -> str:
        return "StartFigmaLookup"

    def execute(self, context: dex.Context, input: FigmaLookupRequest) -> dex.StepDecision:
        if not input.file_key:
            return dex.force_fail("fileKey is required")
        _FILE_KEY.set(context, input.file_key)
        return dex.go_to(dex.StepRef(_FIRST_STEP_TYPE), input)


# dex:group group-id:figma-lookup group-label:"Figma lookup"
# dex:explanation text:"Continue from the file to its comments."
class _AfterFirst(dex.Step[QueryResult[File]]):
    def get_step_type(self) -> str:
        return "AfterFigmaFirst"

    def execute(self, context: dex.Context, input: QueryResult[File]) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_SECOND_STEP_TYPE), GetFileCommentsInput(_FILE_KEY.get(context)))


# dex:group group-id:figma-lookup group-label:"Figma lookup"
# dex:explanation text:"Complete with the comments Figma returned."
class _CompleteLookup(dex.Step[QueryResult[FileComments]]):
    def get_step_type(self) -> str:
        return "CompleteFigmaLookup"

    def execute(self, context: dex.Context, input: QueryResult[FileComments]) -> dex.StepDecision:
        return dex.graceful_complete(FigmaLookupOutcome(input.value.comments))


class FigmaLookupFlow(dex.Flow[FigmaLookupRequest]):
    """Read a Figma file and its comments through the static figma connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Figma lookup requires the static figma connection")
        self._start = _StartRequest()
        self._after_first = _AfterFirst()
        self._complete = _CompleteLookup()
        self._file = new_get_file_step(
            step_type=_FIRST_STEP_TYPE,
            step_input_type=FigmaLookupRequest,
            annotations={**_ANNOTATION_GROUP, "explanation": "Read Figma records."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_file_input,
            found=self._after_first,
        )
        self._comments = new_get_file_comments_step(
            step_type=_SECOND_STEP_TYPE,
            step_input_type=GetFileCommentsInput,
            annotations={**_ANNOTATION_GROUP, "explanation": "Read the related Figma resource."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_file_comments_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[FigmaLookupRequest]:
        """Register the start, both Figma reads, and their result Steps."""
        return dex.StepList.start_step(self._start).other_steps(
            self._file, self._after_first, self._comments, self._complete
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the requested file key between the two reads."""
        return dex.PersistenceSchema.of(_FILE_KEY)
