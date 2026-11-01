# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Netlify lookup Flow using the listSites Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.netlify import Connection, ListSitesInput, SitesPage, new_list_sites_step

FLOW_TYPE = "NetlifyLookup"
CONNECTION_NAME = "netlify"
_OPERATION_STEP_TYPE = "ListSitesNetlify"
_REQUEST = dex.Attribute("netlify-lookup-request", ListSitesInput)
_OUTPUT = dex.Attribute("netlify-lookup-output", SitesPage)


def map_to_operation_input(request: ListSitesInput) -> ListSitesInput:
    """List sites; the request carries no parameters."""
    return request


# dex:group group-id:netlify-lookup group-label:"Netlify lookup"
# dex:explanation text:"Record the lookup request before listing Netlify sites."
class _StartLookup(dex.Step[ListSitesInput]):
    def get_step_type(self) -> str:
        return "StartNetlifyLookup"

    def execute(self, context: dex.Context, input: ListSitesInput) -> dex.StepDecision:
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef(_OPERATION_STEP_TYPE), input)


# dex:group group-id:netlify-lookup group-label:"Netlify lookup"
# dex:explanation text:"Record the sites and complete the Flow."
class _CompleteLookup(dex.Step[QueryResult[SitesPage]]):
    def get_step_type(self) -> str:
        return "CompleteNetlifyLookup"

    def execute(self, context: dex.Context, input: QueryResult[SitesPage]) -> dex.StepDecision:
        _OUTPUT.set(context, input.value)
        return dex.graceful_complete(input.value)


class NetlifyLookupFlow(dex.Flow[ListSitesInput]):
    """List Netlify sites through the static netlify connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Netlify lookup requires the static netlify connection")
        self._start = _StartLookup()
        self._complete = _CompleteLookup()
        self._list = new_list_sites_step(
            step_type=_OPERATION_STEP_TYPE,
            step_input_type=ListSitesInput,
            annotations={
                "groupId": "netlify",
                "groupLabel": "Netlify",
                "explanation": "List sites visible to the Netlify token.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ListSitesInput]:
        """Register the start, provider list, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._list, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the request and site page."""
        return dex.PersistenceSchema.of(_REQUEST, _OUTPUT)
