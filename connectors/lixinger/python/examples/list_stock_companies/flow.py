# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read lixinger data through the static Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.lixinger import (
    Connection,
    ListStockCompaniesInput,
    ListStockCompaniesOutput,
    new_list_stock_companies_step,
)

CONNECTION_NAME = "lixinger"
_OUTCOME = dex.Attribute("lixinger-list_stock_companies-result", ListStockCompaniesOutput)


def map_to_operation_input(input: ListStockCompaniesInput) -> ListStockCompaniesInput:
    """Pass the typed request to the provider Query."""
    return input


# dex:group group-id:list-stock-companies group-label:"ListStockCompanies"
# dex:explanation text:"Start the lixinger Query."
class _StartRequest(dex.Step[ListStockCompaniesInput]):
    def get_step_type(self) -> str:
        return "StartListStockCompanies"

    def execute(self, context: dex.Context, input: ListStockCompaniesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListStockCompanies"), input)


# dex:group group-id:list-stock-companies group-label:"ListStockCompanies"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[ListStockCompaniesOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListStockCompanies"

    def execute(self, context: dex.Context, input: QueryResult[ListStockCompaniesOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListStockCompaniesFlow(dex.Flow[ListStockCompaniesInput]):
    """Run listStockCompanies using the static `lixinger` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("lixinger example requires the static connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._query = new_list_stock_companies_step(
            step_type="CallListStockCompanies",
            step_input_type=ListStockCompaniesInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "lixinger",
                "explanation": "Call the lixinger listStockCompanies Query.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "LixingerListStockCompanies"

    def get_steps(self) -> dex.StepList[ListStockCompaniesInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the typed provider output."""
        return dex.PersistenceSchema.of(_OUTCOME)
