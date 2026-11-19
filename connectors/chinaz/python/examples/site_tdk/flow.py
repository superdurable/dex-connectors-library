# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read public site TDK metadata in a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.chinaz import (
    Connection,
    GetSiteTDKInput,
    SiteTDK,
    new_get_site_tdk_step,
)

CONNECTION_NAME = "chinaz"
_OUTCOME = dex.Attribute("chinaz-site-tdk-outcome", SiteTDK)


def map_to_operation_input(input: GetSiteTDKInput) -> GetSiteTDKInput:
    """Pass the domain to the provider Query."""
    return input


# dex:group group-id:lookup group-label:"Site TDK"
# dex:explanation text:"Start reading public TDK metadata for the requested domain."
class _StartLookup(dex.Step[GetSiteTDKInput]):
    def get_step_type(self) -> str:
        return "StartSiteTDK"

    def execute(self, context: dex.Context, input: GetSiteTDKInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetSiteTDKQuery"), input)


# dex:group group-id:lookup group-label:"Site TDK"
# dex:explanation text:"Persist the site TDK and complete the Flow."
class _CompleteLookup(dex.Step[QueryResult[SiteTDK]]):
    def get_step_type(self) -> str:
        return "CompleteSiteTDK"

    def execute(self, context: dex.Context, input: QueryResult[SiteTDK]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class SiteTDKFlow(dex.Flow[GetSiteTDKInput]):
    """Durably read site metadata using the static Chinaz connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("site TDK requires the static Chinaz connection")
        self._start = _StartLookup()
        self._complete = _CompleteLookup()
        self._query = new_get_site_tdk_step(
            step_type="GetSiteTDKQuery",
            step_input_type=GetSiteTDKInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Chinaz",
                "explanation": "Call Chinaz getSiteTDK with the configured API Key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "ChinazSiteTDK"

    def get_steps(self) -> dex.StepList[GetSiteTDKInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable site TDK Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
