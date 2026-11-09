# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable TikTok campaign discovery Flow using listCampaigns."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.tiktok import (
    CampaignsPage,
    Connection,
    ListCampaignsInput,
    new_list_campaigns_step,
)

FLOW_TYPE = "TikTokCampaignDiscoveryPython"
CONNECTION_NAME = "tiktok-business"
_CALL_STEP_TYPE = "CallTikTok"
_RESULT = dex.Attribute("tiktok-campaigns", CampaignsPage)


def map_to_operation_input(request: ListCampaignsInput) -> ListCampaignsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:tiktok group-label:"TikTok"
# dex:explanation text:"Start the TikTok request."
class _StartRequest(dex.Step[ListCampaignsInput]):
    def get_step_type(self) -> str:
        return "StartTikTokRequest"

    def execute(self, context: dex.Context, input: ListCampaignsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallTikTok"), input)


# dex:group group-id:tiktok group-label:"TikTok"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[CampaignsPage]]):
    def get_step_type(self) -> str:
        return "CompleteTikTokRequest"

    def execute(self, context: dex.Context, input: QueryResult[CampaignsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CampaignDiscoveryFlow(dex.Flow[ListCampaignsInput]):
    """Read one campaign page through the static tiktok connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("campaign discovery requires the static tiktok connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_campaigns_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListCampaignsInput,
            annotations={
                "groupId": "tiktok",
                "groupLabel": "TikTok",
                "explanation": "Read one campaign page from the connected TikTok account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "TikTokCampaignDiscoveryPython"

    def get_steps(self) -> dex.StepList[ListCampaignsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
