# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable ZendeskSell lead discovery Flow using the listLeads Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zendesk_sell import (
    Connection,
    LeadsPage,
    ListLeadsInput,
    new_list_leads_step,
)

FLOW_TYPE = "ZendeskSellLeadDiscovery"
CONNECTION_NAME = "zendesk-sell"
_CALL_STEP_TYPE = "CallListLeads"
_RESULT = dex.Attribute("zendesk-sell-leads-page", LeadsPage)


def map_to_operation_input(request: ListLeadsInput) -> ListLeadsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:zendesk-sell group-label:"Zendesk Sell"
# dex:explanation text:"Start the Zendesk Sell request."
class _StartRequest(dex.Step[ListLeadsInput]):
    def get_step_type(self) -> str:
        return "StartListLeads"

    def execute(self, context: dex.Context, input: ListLeadsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListLeads"), input)


# dex:group group-id:zendesk-sell group-label:"Zendesk Sell"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[LeadsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListLeads"

    def execute(self, context: dex.Context, input: QueryResult[LeadsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class LeadDiscoveryFlow(dex.Flow[ListLeadsInput]):
    """Run one listLeads read through the static zendesk-sell connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("lead discovery requires the static zendesk-sell connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_leads_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListLeadsInput,
            annotations={
                "groupId": "zendesk-sell",
                "groupLabel": "Zendesk Sell",
                "explanation": "List lead identities visible to the connected Zendesk Sell user.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "ZendeskSellLeadDiscovery"

    def get_steps(self) -> dex.StepList[ListLeadsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
