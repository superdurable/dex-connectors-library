# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Freshsales contact discovery Flow using the listContactViews Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.freshworks_freshsales import (
    Connection,
    ContactViews,
    ListContactViewsInput,
    new_list_contact_views_step,
)

FLOW_TYPE = "FreshsalesContactViewDiscovery"
CONNECTION_NAME = "freshsales"
_CALL_STEP_TYPE = "CallListContactViews"
_RESULT = dex.Attribute("freshsales-contact-views", ContactViews)


def map_to_operation_input(request: ListContactViewsInput) -> ListContactViewsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:freshsales group-label:"Freshsales"
# dex:explanation text:"Start the Freshsales request."
class _StartRequest(dex.Step[ListContactViewsInput]):
    def get_step_type(self) -> str:
        return "StartListContactViews"

    def execute(self, context: dex.Context, input: ListContactViewsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListContactViews"), input)


# dex:group group-id:freshsales group-label:"Freshsales"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ContactViews]]):
    def get_step_type(self) -> str:
        return "CompleteListContactViews"

    def execute(self, context: dex.Context, input: QueryResult[ContactViews]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ContactViewDiscoveryFlow(dex.Flow[ListContactViewsInput]):
    """Run one listContactViews read through the static freshsales connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("contact discovery requires the static freshsales connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_contact_views_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListContactViewsInput,
            annotations={
                "groupId": "freshsales",
                "groupLabel": "Freshsales",
                "explanation": "List contact views visible to the connected Freshsales user.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "FreshsalesContactViewDiscovery"

    def get_steps(self) -> dex.StepList[ListContactViewsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
