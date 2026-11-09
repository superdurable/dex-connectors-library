# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Google Analytics 4 property lookup Flow using the getProperty Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_analytics import (
    Connection,
    GetPropertyInput,
    Property,
    new_get_property_step,
)

FLOW_TYPE = "GoogleAnalytics4PropertyPython"
CONNECTION_NAME = "google-analytics-4"
_CALL_STEP_TYPE = "CallGetProperty"
_RESULT = dex.Attribute("google-analytics-4-python-property", Property)


def map_to_operation_input(request: GetPropertyInput) -> GetPropertyInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:google-analytics-4 group-label:"Google Analytics 4"
# dex:explanation text:"Start the Google Analytics 4 request."
class _StartRequest(dex.Step[GetPropertyInput]):
    def get_step_type(self) -> str:
        return "StartGetProperty"

    def execute(self, context: dex.Context, input: GetPropertyInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetProperty"), input)


# dex:group group-id:google-analytics-4 group-label:"Google Analytics 4"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[Property]]):
    def get_step_type(self) -> str:
        return "CompleteGetProperty"

    def execute(self, context: dex.Context, input: QueryResult[Property]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class PropertyFlow(dex.Flow[GetPropertyInput]):
    """Run one getProperty read through the static google-analytics-4 connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the property lookup Flow requires the static google-analytics-4 connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_property_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetPropertyInput,
            annotations={
                "groupId": "google-analytics-4",
                "groupLabel": "Google Analytics 4",
                "explanation": "Read Google Analytics 4 property metadata.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "GoogleAnalytics4PropertyPython"

    def get_steps(self) -> dex.StepList[GetPropertyInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
