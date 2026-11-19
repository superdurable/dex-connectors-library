# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Okki listCustomerFields Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.okki import (
    Connection,
    CustomerFields,
    ListCustomerFieldsInput,
    new_list_customer_fields_step,
)

CONNECTION_NAME = "okki"
_RESULT = dex.Attribute("okki-result", CustomerFields)


def map_to_operation_input(request: ListCustomerFieldsInput) -> ListCustomerFieldsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:okki group-label:"Okki"
# dex:explanation text:"Start the Okki query."
class _StartQuery(dex.Step[ListCustomerFieldsInput]):
    def execute(self, context: dex.Context, input: ListCustomerFieldsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:okki group-label:"Okki"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[CustomerFields]]):
    def execute(self, context: dex.Context, input: QueryResult[CustomerFields]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListCustomerFieldsFlow(dex.Flow[ListCustomerFieldsInput]):
    """Run one Okki query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Okki Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_customer_fields_step(
            step_type="CallProvider",
            step_input_type=ListCustomerFieldsInput,
            annotations={
                "groupId": "okki",
                "groupLabel": "Okki",
                "explanation": "Run Okki listCustomerFields.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListCustomerFieldsOkki"

    def get_steps(self) -> dex.StepList[ListCustomerFieldsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
