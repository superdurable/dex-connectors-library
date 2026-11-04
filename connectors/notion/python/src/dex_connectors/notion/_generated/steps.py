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
from ..models import CreatePageInput, CreatePageOutput, GetPageInput, GetPageOutput, QueryDatabaseInput, QueryDatabaseOutput, SearchInput, SearchOutput, UpdatePagePropertiesInput, UpdatePagePropertiesOutput

StepInput = TypeVar("StepInput")

def new_search_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SearchInput],
    searched: dex.StepTarget[QueryResult[SearchOutput]],
    no_match: dex.StepTarget[QueryResult[SearchOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[SearchOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[SearchOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[SearchOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[SearchOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, SearchInput, SearchOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Notion Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Notion Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Notion Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "searched": searched,
    }
    if no_match is not None:
        branches["noMatch"] = no_match
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
            operation=_bound_operation(connection, "search"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_search_step, '__module__', 'dex_connectors.notion.steps')

def new_query_database_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], QueryDatabaseInput],
    queried: dex.StepTarget[QueryResult[QueryDatabaseOutput]],
    not_found: dex.StepTarget[QueryResult[QueryDatabaseOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[QueryDatabaseOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[QueryDatabaseOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[QueryDatabaseOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[QueryDatabaseOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, QueryDatabaseInput, QueryDatabaseOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Notion Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Notion Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Notion Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "queried": queried,
    }
    if not_found is not None:
        branches["notFound"] = not_found
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
            operation=_bound_operation(connection, "query_database"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_query_database_step, '__module__', 'dex_connectors.notion.steps')

def new_get_page_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetPageInput],
    found: dex.StepTarget[QueryResult[GetPageOutput]],
    not_found: dex.StepTarget[QueryResult[GetPageOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[GetPageOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[GetPageOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[GetPageOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[GetPageOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetPageInput, GetPageOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Notion Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Notion Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Notion Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if not_found is not None:
        branches["notFound"] = not_found
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
            operation=_bound_operation(connection, "get_page"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_page_step, '__module__', 'dex_connectors.notion.steps')

def new_create_page_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreatePageInput],
    created: dex.StepTarget[MutationResult[CreatePageOutput]],
    not_found: dex.StepTarget[MutationResult[CreatePageOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[CreatePageOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[CreatePageOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[CreatePageOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreatePageOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreatePageOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreatePageInput, CreatePageOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Notion Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Notion Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Notion Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "create_page"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_page_step, '__module__', 'dex_connectors.notion.steps')

def new_update_page_properties_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdatePagePropertiesInput],
    updated: dex.StepTarget[MutationResult[UpdatePagePropertiesOutput]],
    not_found: dex.StepTarget[MutationResult[UpdatePagePropertiesOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdatePagePropertiesOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpdatePagePropertiesOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdatePagePropertiesOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdatePagePropertiesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdatePagePropertiesInput, UpdatePagePropertiesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Notion Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Notion Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Notion Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "updated": updated,
    }
    if not_found is not None:
        branches["notFound"] = not_found
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
            operation=_bound_operation(connection, "update_page_properties"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_page_properties_step, '__module__', 'dex_connectors.notion.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
