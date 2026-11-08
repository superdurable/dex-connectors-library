# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Chargebee subscription inspection Flow using getSubscription."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.chargebee import (
    Connection,
    GetSubscriptionInput,
    SubscriptionStatus,
    new_get_subscription_step,
)

FLOW_TYPE = "ChargebeeSubscriptionInspectionPython"
CONNECTION_NAME = "chargebee"
_CALL_STEP_TYPE = "CallChargebee"
_RESULT = dex.Attribute("chargebee-subscription", SubscriptionStatus)


def map_to_operation_input(request: GetSubscriptionInput) -> GetSubscriptionInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:chargebee group-label:"Chargebee"
# dex:explanation text:"Start the Chargebee request."
class _StartRequest(dex.Step[GetSubscriptionInput]):
    def get_step_type(self) -> str:
        return "StartChargebeeRequest"

    def execute(self, context: dex.Context, input: GetSubscriptionInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallChargebee"), input)


# dex:group group-id:chargebee group-label:"Chargebee"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[SubscriptionStatus]]):
    def get_step_type(self) -> str:
        return "CompleteChargebeeRequest"

    def execute(self, context: dex.Context, input: QueryResult[SubscriptionStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SubscriptionInspectionFlow(dex.Flow[GetSubscriptionInput]):
    """Read one subscription through the static chargebee connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("subscription inspection requires the static chargebee connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_subscription_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetSubscriptionInput,
            annotations={
                "groupId": "chargebee",
                "groupLabel": "Chargebee",
                "explanation": "Read one subscription from the connected Chargebee account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "ChargebeeSubscriptionInspectionPython"

    def get_steps(self) -> dex.StepList[GetSubscriptionInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
