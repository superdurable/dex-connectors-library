# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Productboard feature discovery Flow using listFeatures."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.productboard import (
    Connection,
    FeaturesPage,
    ListFeaturesInput,
    new_list_features_step,
)

FLOW_TYPE = "ProductboardFeatureDiscoveryPython"
CONNECTION_NAME = "productboard"
_CALL_STEP_TYPE = "CallProductboard"
_RESULT = dex.Attribute("productboard-features", FeaturesPage)


def map_to_operation_input(request: ListFeaturesInput) -> ListFeaturesInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:productboard group-label:"Productboard"
# dex:explanation text:"Start the Productboard request."
class _StartRequest(dex.Step[ListFeaturesInput]):
    def get_step_type(self) -> str:
        return "StartProductboardRequest"

    def execute(self, context: dex.Context, input: ListFeaturesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProductboard"), input)


# dex:group group-id:productboard group-label:"Productboard"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[FeaturesPage]]):
    def get_step_type(self) -> str:
        return "CompleteProductboardRequest"

    def execute(self, context: dex.Context, input: QueryResult[FeaturesPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class FeatureDiscoveryFlow(dex.Flow[ListFeaturesInput]):
    """Read one feature page through the static productboard connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("feature discovery requires the static productboard connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_features_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListFeaturesInput,
            annotations={
                "groupId": "productboard",
                "groupLabel": "Productboard",
                "explanation": "Read one feature page from the connected Productboard account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "ProductboardFeatureDiscoveryPython"

    def get_steps(self) -> dex.StepList[ListFeaturesInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
