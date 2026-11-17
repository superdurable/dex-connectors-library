# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List Aqara devices through one durable Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.aqara import (
    Connection,
    DevicePage,
    ListDevicesInput,
    new_list_devices_step,
)

CONNECTION_NAME = "aqara"
_RESULT = dex.Attribute("aqara-device-list", DevicePage)


def map_to_operation_input(input: ListDevicesInput) -> ListDevicesInput:
    """Forward the requested page to the Aqara Query."""
    return input


# dex:group group-id:devices group-label:"Devices"
# dex:explanation text:"Start the Aqara device list query."
class _StartList(dex.Step[ListDevicesInput]):
    def get_step_type(self) -> str:
        return "StartAqaraDeviceList"

    def execute(self, context: dex.Context, input: ListDevicesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListAqaraDevices"), input)


# dex:group group-id:devices group-label:"Devices"
# dex:explanation text:"Persist the returned Aqara devices."
class _CompleteList(dex.Step[QueryResult[DevicePage]]):
    def get_step_type(self) -> str:
        return "CompleteAqaraDeviceList"

    def execute(self, context: dex.Context, input: QueryResult[DevicePage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DeviceListFlow(dex.Flow[ListDevicesInput]):
    """Durably list devices through the static Aqara connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("device list requires the static Aqara connection")
        self._start = _StartList()
        self._complete = _CompleteList()
        self._query = new_list_devices_step(
            step_type="ListAqaraDevices",
            step_input_type=ListDevicesInput,
            annotations={
                "groupId": "devices",
                "groupLabel": "Devices",
                "explanation": "Read a bounded device page with the configured Aqara credentials.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable device list Flow identity."""
        return "AqaraDeviceList"

    def get_steps(self) -> dex.StepList[ListDevicesInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable device list Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
