# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Meta Ads Insights ad lookup Flow using the getAd Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.meta_ads_insights import (
    AdMetadata,
    Connection,
    GetAdInput,
    new_get_ad_step,
)

FLOW_TYPE = "MetaAdsInsightsAdInspectionPython"
CONNECTION_NAME = "meta-ads-insights"
_CALL_STEP_TYPE = "CallGetAd"
_RESULT = dex.Attribute("meta-ads-insights-python-ad-metadata", AdMetadata)


def map_to_operation_input(request: GetAdInput) -> GetAdInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:meta-ads-insights group-label:"Meta Ads Insights"
# dex:explanation text:"Start the Meta Ads Insights request."
class _StartRequest(dex.Step[GetAdInput]):
    def get_step_type(self) -> str:
        return "StartGetAd"

    def execute(self, context: dex.Context, input: GetAdInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetAd"), input)


# dex:group group-id:meta-ads-insights group-label:"Meta Ads Insights"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[AdMetadata]]):
    def get_step_type(self) -> str:
        return "CompleteGetAd"

    def execute(self, context: dex.Context, input: QueryResult[AdMetadata]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AdInspectionFlow(dex.Flow[GetAdInput]):
    """Run one getAd read through the static meta-ads-insights connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the ad lookup Flow requires the static meta-ads-insights connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_ad_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetAdInput,
            annotations={
                "groupId": "meta-ads-insights",
                "groupLabel": "Meta Ads Insights",
                "explanation": "Read the ad name and delivery status using the Marketing API token.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "MetaAdsInsightsAdInspectionPython"

    def get_steps(self) -> dex.StepList[GetAdInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
