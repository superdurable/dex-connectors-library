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
    StepAnnotations,
)

from ..connection import Connection
from ..models import SetUserPropertiesInput, UserPropertiesResult

StepInput = TypeVar("StepInput")

def new_set_user_properties_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SetUserPropertiesInput],
    accepted: dex.StepTarget[MutationResult[UserPropertiesResult]],
    provider_rejected: dex.StepTarget[MutationResult[UserPropertiesResult]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UserPropertiesResult]] | None = None,
    defect: dex.StepTarget[MutationResult[UserPropertiesResult]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UserPropertiesResult]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, SetUserPropertiesInput, UserPropertiesResult]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Heap Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Heap Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Heap Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "accepted": accepted,
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
            operation=_bound_operation(connection, "set_user_properties"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_set_user_properties_step, '__module__', 'dex_connectors.heap.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
