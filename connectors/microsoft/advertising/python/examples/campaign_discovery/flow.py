# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Microsoft Advertising campaign Flow using the listCampaigns Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.microsoft_advertising import (
    CampaignsPage,
    Connection,
    ListCampaignsInput,
    new_list_campaigns_step,
)

FLOW_TYPE = "MicrosoftAdvertisingCampaignDiscoveryPython"
CONNECTION_NAME = "microsoft-advertising"
_CALL_STEP_TYPE = "CallListCampaigns"
_RESULT = dex.Attribute("microsoft-advertising-python-campaigns", CampaignsPage)


def map_to_operation_input(request: ListCampaignsInput) -> ListCampaignsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:microsoft-advertising group-label:"Microsoft Advertising"
# dex:explanation text:"Start the Microsoft Advertising campaign request."
class _StartRequest(dex.Step[ListCampaignsInput]):
    def get_step_type(self) -> str:
        return "StartListCampaigns"

    def execute(self, context: dex.Context, input: ListCampaignsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListCampaigns"), input)


# dex:group group-id:microsoft-advertising group-label:"Microsoft Advertising"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[CampaignsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListCampaigns"

    def execute(self, context: dex.Context, input: QueryResult[CampaignsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CampaignFlow(dex.Flow[ListCampaignsInput]):
    """Run one listCampaigns read through the static Microsoft Advertising connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("campaign discovery requires the static microsoft-advertising connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_campaigns_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListCampaignsInput,
            annotations={
                "groupId": "microsoft-advertising",
                "groupLabel": "Microsoft Advertising",
                "explanation": "List campaigns in the selected advertising account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "MicrosoftAdvertisingCampaignDiscoveryPython"

    def get_steps(self) -> dex.StepList[ListCampaignsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
