# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Nimble contact ID discovery Flow using the listContactIDs Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.nimble import (
    Connection,
    ContactIDsPage,
    ListContactIDsInput,
    new_list_contact_ids_step,
)

FLOW_TYPE = "NimbleContactIDDiscovery"
CONNECTION_NAME = "nimble"
_CALL_STEP_TYPE = "CallListContactIDs"
_RESULT = dex.Attribute("nimble-contact-ids-page", ContactIDsPage)


def map_to_operation_input(request: ListContactIDsInput) -> ListContactIDsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:nimble group-label:"Nimble"
# dex:explanation text:"Start the Nimble request."
class _StartRequest(dex.Step[ListContactIDsInput]):
    def get_step_type(self) -> str:
        return "StartListContactIDs"

    def execute(self, context: dex.Context, input: ListContactIDsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListContactIDs"), input)


# dex:group group-id:nimble group-label:"Nimble"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ContactIDsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListContactIDs"

    def execute(self, context: dex.Context, input: QueryResult[ContactIDsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ContactIDDiscoveryFlow(dex.Flow[ListContactIDsInput]):
    """Run one listContactIDs read through the static nimble connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("contact ID discovery requires the static nimble connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_contact_ids_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListContactIDsInput,
            annotations={
                "groupId": "nimble",
                "groupLabel": "Nimble",
                "explanation": "List contact IDs visible to the connected Nimble user.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "NimbleContactIDDiscovery"

    def get_steps(self) -> dex.StepList[ListContactIDsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
