# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Tuya provider result in a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.tuya import (
    Connection,
    GetDeviceDetailsInput,
    GetDeviceDetailsOutput,
    new_get_device_details_step,
)

CONNECTION_NAME = "tuya"
_RESULT = dex.Attribute("tuya-device-details", GetDeviceDetailsOutput)


def map_to_operation_input(input: GetDeviceDetailsInput) -> GetDeviceDetailsInput:
    """Pass the public Flow input to the provider Query."""
    return input


# dex:group group-id:tuya group-label:"Tuya"
# dex:explanation text:"Start the Tuya device metadata Query for this Flow."
class _Start(dex.Step[GetDeviceDetailsInput]):
    def get_step_type(self) -> str:
        return "StartDeviceDetailsFlow"

    def execute(self, context: dex.Context, input: GetDeviceDetailsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetDeviceDetailsQuery"), input)


# dex:group group-id:tuya group-label:"Tuya"
# dex:explanation text:"Persist safe device metadata and complete the Flow."
class _Complete(dex.Step[QueryResult[GetDeviceDetailsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteDeviceDetails"

    def execute(self, context: dex.Context, input: QueryResult[GetDeviceDetailsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DeviceDetailsFlow(dex.Flow[GetDeviceDetailsInput]):
    """Call Tuya through the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Tuya connection name differs")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_get_device_details_step(
            step_type="GetDeviceDetailsQuery",
            step_input_type=GetDeviceDetailsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Tuya",
                "explanation": "Read non-secret Tuya device metadata.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable application Flow identity."""
        return "TuyaDeviceDetailsFlow"

    def get_steps(self) -> dex.StepList[GetDeviceDetailsInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
