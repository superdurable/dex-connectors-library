# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Google Search Console site listing Flow using the listSites Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_search_console import (
    Connection,
    ListSitesInput,
    SitesPage,
    new_list_sites_step,
)

FLOW_TYPE = "GoogleSearchConsoleSitesPython"
CONNECTION_NAME = "google-search-console"
_CALL_STEP_TYPE = "CallListSites"
_RESULT = dex.Attribute("google-search-console-python-sites", SitesPage)


def map_to_operation_input(request: ListSitesInput) -> ListSitesInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:google-search-console group-label:"Google Search Console"
# dex:explanation text:"Start the Google Search Console request."
class _StartRequest(dex.Step[ListSitesInput]):
    def get_step_type(self) -> str:
        return "StartListSites"

    def execute(self, context: dex.Context, input: ListSitesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListSites"), input)


# dex:group group-id:google-search-console group-label:"Google Search Console"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[SitesPage]]):
    def get_step_type(self) -> str:
        return "CompleteListSites"

    def execute(self, context: dex.Context, input: QueryResult[SitesPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SitesFlow(dex.Flow[ListSitesInput]):
    """Run one listSites read through the static google-search-console connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the site listing Flow requires the static google-search-console connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_sites_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListSitesInput,
            annotations={
                "groupId": "google-search-console",
                "groupLabel": "Google Search Console",
                "explanation": "List visible Google Search Console property URLs and permissions.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "GoogleSearchConsoleSitesPython"

    def get_steps(self) -> dex.StepList[ListSitesInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
