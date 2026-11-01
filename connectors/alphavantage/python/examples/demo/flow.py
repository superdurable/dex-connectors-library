# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Alpha Vantage demo Flow using the getGlobalQuote Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.alphavantage import (
    Connection,
    GlobalQuoteOutput,
    StockInput,
    new_get_global_quote_step,
)

FLOW_TYPE = "AlphaVantageDemo"
CONNECTION_NAME = "alphavantage"
_OPERATION_STEP_TYPE = "CallGetGlobalQuote"
_OUTCOME = dex.Attribute("alphavantage-demo-outcome", GlobalQuoteOutput)


def map_to_operation_input(input: StockInput) -> StockInput:
    """Read the requested symbol unchanged."""
    return input


# dex:group group-id:demo group-label:"Demo"
# dex:explanation text:"Start the quote lookup with the requested symbol."
class _StartRequest(dex.Step[StockInput]):
    def get_step_type(self) -> str:
        return "StartGetGlobalQuote"

    def execute(self, context: dex.Context, input: StockInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_OPERATION_STEP_TYPE), input)


# dex:group group-id:demo group-label:"Demo"
# dex:explanation text:"Record the quote and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[GlobalQuoteOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetGlobalQuote"

    def execute(self, context: dex.Context, input: QueryResult[GlobalQuoteOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class DemoFlow(dex.Flow[StockInput]):
    """Read a stock quote through the static alphavantage connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("demo requires the static alphavantage connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_get_global_quote_step(
            step_type=_OPERATION_STEP_TYPE,
            step_input_type=StockInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Alpha Vantage",
                "explanation": "Call the provider's getGlobalQuote operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[StockInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the quote outcome."""
        return dex.PersistenceSchema.of(_OUTCOME)
