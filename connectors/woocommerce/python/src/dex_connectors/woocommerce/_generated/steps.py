# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation-specific Dex Step factories."""

from collections.abc import Callable, Mapping
from typing import Any, TypeVar

import dex
from dex_connectors.sdk import (
    QueryResult,
    QueryStep,
    QueryStepConfig,
    StepAnnotations,
)

from ..connection import Connection
from ..models import GetOrderInput, ListOrdersInput, Order, OrderPage

StepInput = TypeVar("StepInput")

def new_list_orders_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListOrdersInput],
    listed: dex.StepTarget[QueryResult[OrderPage]],
    not_found: dex.StepTarget[QueryResult[OrderPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[OrderPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[OrderPage]] | None = None,
    defect: dex.StepTarget[QueryResult[OrderPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[OrderPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListOrdersInput, OrderPage]:
    """Create the list_orders Step with the manifest branch identities."""
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("WooCommerce Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("WooCommerce Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("WooCommerce Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
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
            operation=_bound_operation(connection, "list_orders"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_orders_step, '__module__', 'dex_connectors.woocommerce.steps')

def new_get_order_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetOrderInput],
    found: dex.StepTarget[QueryResult[Order]],
    not_found: dex.StepTarget[QueryResult[Order]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Order]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Order]] | None = None,
    defect: dex.StepTarget[QueryResult[Order]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Order]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetOrderInput, Order]:
    """Create the get_order Step with the manifest branch identities."""
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("WooCommerce Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("WooCommerce Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("WooCommerce Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_order"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_order_step, '__module__', 'dex_connectors.woocommerce.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    """Resolve a bound operation from a connection or its operation collection."""
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
