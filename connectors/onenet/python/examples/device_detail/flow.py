# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable OneNET device-detail Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.onenet import (
    Connection,
    DeviceDetail,
    QueryDeviceDetailInput,
    new_query_device_detail_step,
)

CONNECTION_NAME = "onenet"
_RESULT = dex.Attribute("onenet-device-detail", DeviceDetail)


def map_to_operation_input(request: QueryDeviceDetailInput) -> QueryDeviceDetailInput:
    """Forward the device detail request to OneNET."""
    return request


# dex:group group-id:onenet group-label:"OneNET"
# dex:explanation text:"Start the OneNET device-detail request."
class _StartProfile(dex.Step[QueryDeviceDetailInput]):
    def get_step_type(self) -> str:
        return "StartOneNETDeviceDetail"

    def execute(self, context: dex.Context, input: QueryDeviceDetailInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallOneNET"), input)


# dex:group group-id:onenet group-label:"OneNET"
# dex:explanation text:"Persist device detail and complete the Flow."
class _CompleteProfile(dex.Step[QueryResult[DeviceDetail]]):
    def get_step_type(self) -> str:
        return "CompleteOneNETDeviceDetail"

    def execute(self, context: dex.Context, input: QueryResult[DeviceDetail]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DeviceDetailFlow(dex.Flow[QueryDeviceDetailInput]):
    """Read one device detail through the static OneNET connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("DeviceDetailFlow requires the static OneNET connection")
        self._start = _StartProfile()
        self._complete = _CompleteProfile()
        self._call = new_query_device_detail_step(
            step_type="CallOneNET",
            step_input_type=QueryDeviceDetailInput,
            annotations={
                "groupId": "onenet",
                "groupLabel": "OneNET",
                "explanation": "Read one OneNET device.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable device-detail Flow identity."""
        return "OneNETDeviceDetail"

    def get_steps(self) -> dex.StepList[QueryDeviceDetailInput]:
        """Register the device detail Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the device-detail result."""
        return dex.PersistenceSchema.of(_RESULT)
