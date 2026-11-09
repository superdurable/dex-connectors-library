# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Ads page Flow using the listAccessibleCustomers Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_ads import (
    AccessibleCustomers,
    Connection,
    ListAccessibleCustomersInput,
    new_list_accessible_customers_step,
)

FLOW_TYPE = "AdsCustomersListPython"
CONNECTION_NAME = "google-ads"
_CALL_STEP_TYPE = "CallListAccessibleCustomers"
_RESULT = dex.Attribute("google_ads-python-page", AccessibleCustomers)


def map_to_operation_input(request: ListAccessibleCustomersInput) -> ListAccessibleCustomersInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:google-ads group-label:"Ads"
# dex:explanation text:"Start the Ads request."
class _StartRequest(dex.Step[ListAccessibleCustomersInput]):
    def get_step_type(self) -> str:
        return "StartListAccessibleCustomers"

    def execute(self, context: dex.Context, input: ListAccessibleCustomersInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListAccessibleCustomers"), input)


# dex:group group-id:google-ads group-label:"Ads"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[AccessibleCustomers]]):
    def get_step_type(self) -> str:
        return "CompleteListAccessibleCustomers"

    def execute(self, context: dex.Context, input: QueryResult[AccessibleCustomers]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CustomersFlow(dex.Flow[ListAccessibleCustomersInput]):
    """Run one listAccessibleCustomers read through the static google_ads connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the customers lookup Flow requires the static google_ads connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_accessible_customers_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListAccessibleCustomersInput,
            annotations={
                "groupId": "google-ads",
                "groupLabel": "Ads",
                "explanation": "Read one Ads page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AdsCustomersListPython"

    def get_steps(self) -> dex.StepList[ListAccessibleCustomersInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
