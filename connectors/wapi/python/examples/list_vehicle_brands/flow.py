# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Wapi listVehicleBrands Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.wapi import (
    Connection,
    ListVehicleBrandsInput,
    ListVehicleBrandsOutput,
    new_list_vehicle_brands_step,
)

CONNECTION_NAME = "wapi"
_RESULT = dex.Attribute("wapi-result", ListVehicleBrandsOutput)


def map_to_operation_input(request: ListVehicleBrandsInput) -> ListVehicleBrandsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:wapi group-label:"Wapi"
# dex:explanation text:"Start the Wapi query."
class _StartQuery(dex.Step[ListVehicleBrandsInput]):
    def execute(self, context: dex.Context, input: ListVehicleBrandsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:wapi group-label:"Wapi"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListVehicleBrandsOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListVehicleBrandsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListVehicleBrandsFlow(dex.Flow[ListVehicleBrandsInput]):
    """Run one Wapi query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Wapi Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_vehicle_brands_step(
            step_type="CallProvider",
            step_input_type=ListVehicleBrandsInput,
            annotations={
                "groupId": "wapi",
                "groupLabel": "Wapi",
                "explanation": "Run Wapi listVehicleBrands.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListVehicleBrandsWapi"

    def get_steps(self) -> dex.StepList[ListVehicleBrandsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
