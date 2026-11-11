# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from collections.abc import Callable, Mapping
from typing import Any, TypeVar

import dex
from dex_connectors.sdk import (
    MutationResult,
    MutationStep,
    MutationStepConfig,
    QueryResult,
    QueryStep,
    QueryStepConfig,
    StepAnnotations,
)

from ..connection import Connection
from ..models import AddPlaylistItemInput, AddedPlaylistItem, Channel, GetChannelInput, GetVideoInput, ListPlaylistItemsInput, PlaylistItemPage, SearchVideosInput, Video, VideoSearchPage

StepInput = TypeVar("StepInput")

def new_search_videos_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SearchVideosInput],
    searched: dex.StepTarget[QueryResult[VideoSearchPage]],
    quota_exhausted: dex.StepTarget[QueryResult[VideoSearchPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[VideoSearchPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[VideoSearchPage]] | None = None,
    defect: dex.StepTarget[QueryResult[VideoSearchPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[VideoSearchPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, SearchVideosInput, VideoSearchPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("YouTube Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("YouTube Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("YouTube Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "searched": searched,
    }
    if quota_exhausted is not None:
        branches["quotaExhausted"] = quota_exhausted
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "search_videos"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_search_videos_step, '__module__', 'dex_connectors.google_youtube.steps')

def new_get_video_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetVideoInput],
    found: dex.StepTarget[QueryResult[Video]],
    not_found: dex.StepTarget[QueryResult[Video]] | None = None,
    quota_exhausted: dex.StepTarget[QueryResult[Video]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Video]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Video]] | None = None,
    defect: dex.StepTarget[QueryResult[Video]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Video]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetVideoInput, Video]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("YouTube Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("YouTube Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("YouTube Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if quota_exhausted is not None:
        branches["quotaExhausted"] = quota_exhausted
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "get_video"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_video_step, '__module__', 'dex_connectors.google_youtube.steps')

def new_get_channel_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetChannelInput],
    found: dex.StepTarget[QueryResult[Channel]],
    not_found: dex.StepTarget[QueryResult[Channel]] | None = None,
    quota_exhausted: dex.StepTarget[QueryResult[Channel]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Channel]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Channel]] | None = None,
    defect: dex.StepTarget[QueryResult[Channel]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Channel]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetChannelInput, Channel]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("YouTube Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("YouTube Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("YouTube Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if quota_exhausted is not None:
        branches["quotaExhausted"] = quota_exhausted
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "get_channel"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_channel_step, '__module__', 'dex_connectors.google_youtube.steps')

def new_list_playlist_items_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListPlaylistItemsInput],
    listed: dex.StepTarget[QueryResult[PlaylistItemPage]],
    not_found: dex.StepTarget[QueryResult[PlaylistItemPage]] | None = None,
    quota_exhausted: dex.StepTarget[QueryResult[PlaylistItemPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[PlaylistItemPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[PlaylistItemPage]] | None = None,
    defect: dex.StepTarget[QueryResult[PlaylistItemPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[PlaylistItemPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListPlaylistItemsInput, PlaylistItemPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("YouTube Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("YouTube Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("YouTube Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if quota_exhausted is not None:
        branches["quotaExhausted"] = quota_exhausted
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "list_playlist_items"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_playlist_items_step, '__module__', 'dex_connectors.google_youtube.steps')

def new_add_playlist_item_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], AddPlaylistItemInput],
    added: dex.StepTarget[MutationResult[AddedPlaylistItem]],
    not_found: dex.StepTarget[MutationResult[AddedPlaylistItem]] | None = None,
    quota_exhausted: dex.StepTarget[MutationResult[AddedPlaylistItem]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[AddedPlaylistItem]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[AddedPlaylistItem]] | None = None,
    uncertain: dex.StepTarget[MutationResult[AddedPlaylistItem]] | None = None,
    defect: dex.StepTarget[MutationResult[AddedPlaylistItem]] | None = None,
    result_attribute: dex.Attribute[MutationResult[AddedPlaylistItem]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, AddPlaylistItemInput, AddedPlaylistItem]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("YouTube Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("YouTube Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("YouTube Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "added": added,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if quota_exhausted is not None:
        branches["quotaExhausted"] = quota_exhausted
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "add_playlist_item"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_add_playlist_item_step, '__module__', 'dex_connectors.google_youtube.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
