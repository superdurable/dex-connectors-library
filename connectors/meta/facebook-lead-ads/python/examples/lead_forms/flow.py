# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Facebook Lead Ads lead forms Flow using the listLeadForms Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.meta_facebook_lead_ads import (
    Connection,
    LeadFormsPage,
    ListLeadFormsInput,
    new_list_lead_forms_step,
)

FLOW_TYPE = "FacebookLeadForms"
CONNECTION_NAME = "facebook-lead-ads"
_CALL_STEP_TYPE = "CallListLeadForms"
_RESULT = dex.Attribute("facebook-lead-ads-forms-page", LeadFormsPage)


def map_to_operation_input(request: ListLeadFormsInput) -> ListLeadFormsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:facebook-lead-ads group-label:"Facebook Lead Ads"
# dex:explanation text:"Start the Facebook Lead Ads request."
class _StartRequest(dex.Step[ListLeadFormsInput]):
    def get_step_type(self) -> str:
        return "StartListLeadForms"

    def execute(self, context: dex.Context, input: ListLeadFormsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListLeadForms"), input)


# dex:group group-id:facebook-lead-ads group-label:"Facebook Lead Ads"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[LeadFormsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListLeadForms"

    def execute(self, context: dex.Context, input: QueryResult[LeadFormsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class LeadFormsFlow(dex.Flow[ListLeadFormsInput]):
    """Run one listLeadForms read through the static facebook-lead-ads connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the lead forms requires the static facebook-lead-ads connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_lead_forms_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListLeadFormsInput,
            annotations={
                "groupId": "facebook-lead-ads",
                "groupLabel": "Facebook Lead Ads",
                "explanation": "List Page lead-form IDs and names using the Page token.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "FacebookLeadForms"

    def get_steps(self) -> dex.StepList[ListLeadFormsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
