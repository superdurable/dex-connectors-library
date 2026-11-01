# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation-specific Dex Step factories."""

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
from ..models import EventImportResult, ExportEventsInput, ExportedEvents, TrackEventInput

StepInput = TypeVar("StepInput")

def new_track_event_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], TrackEventInput],
    imported: dex.StepTarget[MutationResult[EventImportResult]],
    provider_rejected: dex.StepTarget[MutationResult[EventImportResult]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[EventImportResult]] | None = None,
    defect: dex.StepTarget[MutationResult[EventImportResult]] | None = None,
    result_attribute: dex.Attribute[MutationResult[EventImportResult]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, TrackEventInput, EventImportResult]:
    """Create the track_event Step with the manifest branch identities."""
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Mixpanel Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Mixpanel Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Mixpanel Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "imported": imported,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "track_event"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_track_event_step, '__module__', 'dex_connectors.mixpanel.steps')

def new_export_events_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ExportEventsInput],
    exported: dex.StepTarget[QueryResult[ExportedEvents]],
    provider_rejected: dex.StepTarget[QueryResult[ExportedEvents]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ExportedEvents]] | None = None,
    defect: dex.StepTarget[QueryResult[ExportedEvents]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ExportedEvents]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ExportEventsInput, ExportedEvents]:
    """Create the export_events Step with the manifest branch identities."""
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Mixpanel Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Mixpanel Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Mixpanel Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "exported": exported,
    }
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
            operation=_bound_operation(connection, "export_events"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_export_events_step, '__module__', 'dex_connectors.mixpanel.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    """Resolve a bound operation from a connection or its operation collection."""
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
