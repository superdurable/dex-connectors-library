# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Campaign Monitor client inspection Flow using the listClients Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.campaignmonitor import (
    ClientsList,
    Connection,
    ListClientsInput,
    new_list_clients_step,
)

FLOW_TYPE = "CampaignMonitorClientDiscovery"
CONNECTION_NAME = "campaignmonitor"
_CALL_STEP_TYPE = "CallListClients"
_RESULT = dex.Attribute("campaignmonitor-clients-list", ClientsList)


def map_to_operation_input(request: ListClientsInput) -> ListClientsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:campaignmonitor group-label:"Campaign Monitor"
# dex:explanation text:"Start the Campaign Monitor request."
class _StartRequest(dex.Step[ListClientsInput]):
    def get_step_type(self) -> str:
        return "StartListClients"

    def execute(self, context: dex.Context, input: ListClientsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListClients"), input)


# dex:group group-id:campaignmonitor group-label:"Campaign Monitor"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ClientsList]]):
    def get_step_type(self) -> str:
        return "CompleteListClients"

    def execute(self, context: dex.Context, input: QueryResult[ClientsList]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ClientDiscoveryFlow(dex.Flow[ListClientsInput]):
    """Run one listClients read through the static campaignmonitor connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the client inspection requires the static campaignmonitor connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_clients_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListClientsInput,
            annotations={
                "groupId": "campaignmonitor",
                "groupLabel": "Campaign Monitor",
                "explanation": "List clients accessible to the connected Campaign Monitor account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "CampaignMonitorClientDiscovery"

    def get_steps(self) -> dex.StepList[ListClientsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
