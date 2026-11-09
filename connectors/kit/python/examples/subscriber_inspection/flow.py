# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Kit subscriber inspection Flow using the listSubscribers Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.kit import (
    Connection,
    ListSubscribersInput,
    SubscribersPage,
    new_list_subscribers_step,
)

FLOW_TYPE = "KitSubscriberInspection"
CONNECTION_NAME = "kit"
_CALL_STEP_TYPE = "CallKit"
_RESULT = dex.Attribute("kit-subscribers-page", SubscribersPage)


def map_to_operation_input(request: ListSubscribersInput) -> ListSubscribersInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:kit group-label:"Kit"
# dex:explanation text:"Start the Kit request."
class _StartRequest(dex.Step[ListSubscribersInput]):
    def get_step_type(self) -> str:
        return "StartKitRequest"

    def execute(self, context: dex.Context, input: ListSubscribersInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallKit"), input)


# dex:group group-id:kit group-label:"Kit"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[SubscribersPage]]):
    def get_step_type(self) -> str:
        return "CompleteKitRequest"

    def execute(self, context: dex.Context, input: QueryResult[SubscribersPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SubscriberInspectionFlow(dex.Flow[ListSubscribersInput]):
    """Run one listSubscribers read through the static kit connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the subscriber inspection requires the static kit connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_subscribers_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListSubscribersInput,
            annotations={
                "groupId": "kit",
                "groupLabel": "Kit",
                "explanation": "List subscribers accessible to the connected Kit account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "KitSubscriberInspection"

    def get_steps(self) -> dex.StepList[ListSubscribersInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
