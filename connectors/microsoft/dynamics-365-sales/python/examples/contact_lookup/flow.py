# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Microsoft Dynamics 365 Sales contact lookup Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.microsoft_dynamics_365_sales import (
    Connection,
    Contact,
    FindContactInput,
    new_find_contact_step,
)

FLOW_TYPE = "MicrosoftDynamics365SalesContactLookupPython"
CONNECTION_NAME = "microsoft-dynamics-365-sales"
_CALL_STEP_TYPE = "CallFindContact"
_RESULT = dex.Attribute("dynamics365sales-contact-lookup", Contact)


def map_to_operation_input(request: FindContactInput) -> FindContactInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:dynamics365sales group-label:"Microsoft Dynamics 365 Sales"
# dex:explanation text:"Start the Microsoft Dynamics 365 Sales request."
class _StartRequest(dex.Step[FindContactInput]):
    def get_step_type(self) -> str:
        return "StartFindContactRequest"

    def execute(self, context: dex.Context, input: FindContactInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallFindContact"), input)


# dex:group group-id:dynamics365sales group-label:"Microsoft Dynamics 365 Sales"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[Contact]]):
    def get_step_type(self) -> str:
        return "CompleteFindContactRequest"

    def execute(self, context: dex.Context, input: QueryResult[Contact]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ContactFlow(dex.Flow[FindContactInput]):
    """Find a contact through the static microsoft-dynamics-365-sales connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("contact lookup requires the static microsoft-dynamics-365-sales connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_find_contact_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=FindContactInput,
            annotations={
                "groupId": "dynamics365sales",
                "groupLabel": "Microsoft Dynamics 365 Sales",
                "explanation": "Find one contact by email in Dynamics 365 Sales.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "MicrosoftDynamics365SalesContactLookupPython"

    def get_steps(self) -> dex.StepList[FindContactInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
