# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Odoo partner search Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.odoo import (
    Connection,
    PartnerSearchPage,
    SearchPartnersInput,
    new_search_partners_step,
)

FLOW_TYPE = "OdooPartnerSearchPython"
CONNECTION_NAME = "odoo"
_CALL_STEP_TYPE = "CallSearchPartners"
_RESULT = dex.Attribute("odoo-partner-search", PartnerSearchPage)


def map_to_operation_input(request: SearchPartnersInput) -> SearchPartnersInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:odoo group-label:"Odoo"
# dex:explanation text:"Start the Odoo request."
class _StartRequest(dex.Step[SearchPartnersInput]):
    def get_step_type(self) -> str:
        return "StartOdooRequest"

    def execute(self, context: dex.Context, input: SearchPartnersInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallSearchPartners"), input)


# dex:group group-id:odoo group-label:"Odoo"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[PartnerSearchPage]]):
    def get_step_type(self) -> str:
        return "CompleteOdooRequest"

    def execute(self, context: dex.Context, input: QueryResult[PartnerSearchPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PartnerSearchFlow(dex.Flow[SearchPartnersInput]):
    """Search company partners through the static Odoo connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("partner search requires the static odoo connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_search_partners_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=SearchPartnersInput,
            annotations={
                "groupId": "odoo",
                "groupLabel": "Odoo",
                "explanation": "Search Odoo company partners by name pattern.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "OdooPartnerSearchPython"

    def get_steps(self) -> dex.StepList[SearchPartnersInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
