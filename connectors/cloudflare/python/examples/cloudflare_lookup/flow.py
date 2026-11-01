# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Cloudflare lookup Flow using the listZones Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.cloudflare import Connection, ListZonesInput, ZonesPage, new_list_zones_step

FLOW_TYPE = "CloudflareLookup"
CONNECTION_NAME = "cloudflare"
_OPERATION_STEP_TYPE = "ListZonesCloudflare"
_REQUEST = dex.Attribute("cloudflare-lookup-request", ListZonesInput)
_OUTPUT = dex.Attribute("cloudflare-lookup-output", ZonesPage)


def map_to_operation_input(request: ListZonesInput) -> ListZonesInput:
    """List zones; the request carries no parameters."""
    return request


# dex:group group-id:cloudflare group-label:"Cloudflare"
# dex:explanation text:"Start the Cloudflare lookup with its typed request."
class _StartLookup(dex.Step[ListZonesInput]):
    def get_step_type(self) -> str:
        return "StartCloudflareLookup"

    def execute(self, context: dex.Context, input: ListZonesInput) -> dex.StepDecision:
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef(_OPERATION_STEP_TYPE), input)


# dex:group group-id:cloudflare group-label:"Cloudflare"
# dex:explanation text:"Complete with the Cloudflare result."
class _CompleteLookup(dex.Step[QueryResult[ZonesPage]]):
    def get_step_type(self) -> str:
        return "CompleteCloudflareLookup"

    def execute(self, context: dex.Context, input: QueryResult[ZonesPage]) -> dex.StepDecision:
        _OUTPUT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CloudflareLookupFlow(dex.Flow[ListZonesInput]):
    """List Cloudflare zones through the static cloudflare connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Cloudflare lookup requires the static cloudflare connection")
        self._start = _StartLookup()
        self._complete = _CompleteLookup()
        self._list = new_list_zones_step(
            step_type=_OPERATION_STEP_TYPE,
            step_input_type=ListZonesInput,
            annotations={
                "groupId": "cloudflare",
                "groupLabel": "Cloudflare",
                "explanation": "List the first page of Cloudflare zones.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ListZonesInput]:
        """Register the start, provider list, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._list, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the request and zone page."""
        return dex.PersistenceSchema.of(_REQUEST, _OUTPUT)
