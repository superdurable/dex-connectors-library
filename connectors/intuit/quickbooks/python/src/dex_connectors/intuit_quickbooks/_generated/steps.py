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
from ..models import CreateCustomerInput, CreateCustomerOutput, CreateInvoiceInput, CreateInvoiceOutput, FindCustomerInput, FindCustomerOutput, GetInvoiceInput, Invoice, InvoicePage, ListInvoicesInput, Payment, RecordPaymentInput, SendInvoiceInput

StepInput = TypeVar("StepInput")

def new_find_customer_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], FindCustomerInput],
    found: dex.StepTarget[QueryResult[FindCustomerOutput]],
    not_found: dex.StepTarget[QueryResult[FindCustomerOutput]] | None = None,
    ambiguous: dex.StepTarget[QueryResult[FindCustomerOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[FindCustomerOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[FindCustomerOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[FindCustomerOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[FindCustomerOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, FindCustomerInput, FindCustomerOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("QuickBooks Online Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("QuickBooks Online Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("QuickBooks Online Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if ambiguous is not None:
        branches["ambiguous"] = ambiguous
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
            operation=_bound_operation(connection, "find_customer"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_find_customer_step, '__module__', 'dex_connectors.intuit_quickbooks.steps')

def new_create_customer_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateCustomerInput],
    created: dex.StepTarget[MutationResult[CreateCustomerOutput]],
    name_conflict: dex.StepTarget[MutationResult[CreateCustomerOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[CreateCustomerOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[CreateCustomerOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreateCustomerOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreateCustomerOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateCustomerInput, CreateCustomerOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("QuickBooks Online Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("QuickBooks Online Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("QuickBooks Online Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
    if name_conflict is not None:
        branches["nameConflict"] = name_conflict
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "create_customer"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_customer_step, '__module__', 'dex_connectors.intuit_quickbooks.steps')

def new_create_invoice_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateInvoiceInput],
    created: dex.StepTarget[MutationResult[CreateInvoiceOutput]],
    provider_rejected: dex.StepTarget[MutationResult[CreateInvoiceOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[CreateInvoiceOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreateInvoiceOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreateInvoiceOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateInvoiceInput, CreateInvoiceOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("QuickBooks Online Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("QuickBooks Online Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("QuickBooks Online Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "create_invoice"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_invoice_step, '__module__', 'dex_connectors.intuit_quickbooks.steps')

def new_get_invoice_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetInvoiceInput],
    found: dex.StepTarget[QueryResult[Invoice]],
    not_found: dex.StepTarget[QueryResult[Invoice]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Invoice]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Invoice]] | None = None,
    defect: dex.StepTarget[QueryResult[Invoice]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Invoice]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetInvoiceInput, Invoice]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("QuickBooks Online Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("QuickBooks Online Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("QuickBooks Online Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_invoice"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_invoice_step, '__module__', 'dex_connectors.intuit_quickbooks.steps')

def new_list_invoices_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListInvoicesInput],
    listed: dex.StepTarget[QueryResult[InvoicePage]],
    provider_rejected: dex.StepTarget[QueryResult[InvoicePage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[InvoicePage]] | None = None,
    defect: dex.StepTarget[QueryResult[InvoicePage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[InvoicePage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListInvoicesInput, InvoicePage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("QuickBooks Online Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("QuickBooks Online Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("QuickBooks Online Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
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
            operation=_bound_operation(connection, "list_invoices"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_invoices_step, '__module__', 'dex_connectors.intuit_quickbooks.steps')

def new_send_invoice_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SendInvoiceInput],
    sent: dex.StepTarget[MutationResult[Invoice]],
    provider_rejected: dex.StepTarget[MutationResult[Invoice]] | None = None,
    uncertain: dex.StepTarget[MutationResult[Invoice]] | None = None,
    defect: dex.StepTarget[MutationResult[Invoice]] | None = None,
    result_attribute: dex.Attribute[MutationResult[Invoice]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, SendInvoiceInput, Invoice]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("QuickBooks Online Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("QuickBooks Online Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("QuickBooks Online Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "sent": sent,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "send_invoice"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_send_invoice_step, '__module__', 'dex_connectors.intuit_quickbooks.steps')

def new_record_payment_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], RecordPaymentInput],
    recorded: dex.StepTarget[MutationResult[Payment]],
    provider_rejected: dex.StepTarget[MutationResult[Payment]] | None = None,
    uncertain: dex.StepTarget[MutationResult[Payment]] | None = None,
    defect: dex.StepTarget[MutationResult[Payment]] | None = None,
    result_attribute: dex.Attribute[MutationResult[Payment]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, RecordPaymentInput, Payment]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("QuickBooks Online Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("QuickBooks Online Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("QuickBooks Online Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "recorded": recorded,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "record_payment"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_record_payment_step, '__module__', 'dex_connectors.intuit_quickbooks.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
