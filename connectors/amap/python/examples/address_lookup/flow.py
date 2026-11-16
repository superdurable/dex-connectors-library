# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Resolve one Chinese address through the Amap Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.amap import (
    Connection,
    GeocodeAddressInput,
    GeocodeAddressOutput,
    new_geocode_address_step,
)

CONNECTION_NAME = "amap"
_OUTCOME = dex.Attribute("amap-address-lookup-outcome", GeocodeAddressOutput)


def map_to_operation_input(input: GeocodeAddressInput) -> GeocodeAddressInput:
    """Pass the address request to the provider Query unchanged."""
    return input


# dex:group group-id:lookup group-label:"Address lookup"
# dex:explanation text:"Start geocoding the requested Chinese address."
class _StartLookup(dex.Step[GeocodeAddressInput]):
    def get_step_type(self) -> str:
        return "StartAddressLookup"

    def execute(self, context: dex.Context, input: GeocodeAddressInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GeocodeAddressQuery"), input)


# dex:group group-id:lookup group-label:"Address lookup"
# dex:explanation text:"Save validated Amap location candidates and complete the Flow."
class _CompleteLookup(dex.Step[QueryResult[GeocodeAddressOutput]]):
    def get_step_type(self) -> str:
        return "CompleteAddressLookup"

    def execute(self, context: dex.Context, input: QueryResult[GeocodeAddressOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class AddressLookupFlow(dex.Flow[GeocodeAddressInput]):
    """Durably resolve an address using the static Amap connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("address lookup requires the static Amap connection")
        self._start = _StartLookup()
        self._complete = _CompleteLookup()
        self._query = new_geocode_address_step(
            step_type="GeocodeAddressQuery",
            step_input_type=GeocodeAddressInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Amap",
                "explanation": "Call Amap geocodeAddress with the configured Web Service key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            geocoded=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "AmapAddressLookup"

    def get_steps(self) -> dex.StepList[GeocodeAddressInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable location result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
