# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Tianyancha response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.tianyancha import (
    Connection,
    GetCompanyProfileInput,
    GetCompanyProfileOutput,
    new_get_company_profile_step,
)

CONNECTION_NAME = "tianyancha"
_RESULT = dex.Attribute("tianyancha-query-result", GetCompanyProfileOutput)


def map_to_operation_input(input: GetCompanyProfileInput) -> GetCompanyProfileInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the Tianyancha provider Query."
class _StartQuery(dex.Step[GetCompanyProfileInput]):
    def get_step_type(self) -> str:
        return "StartGetCompanyProfileQuery"

    def execute(self, context: dex.Context, input: GetCompanyProfileInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetCompanyProfileQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated Tianyancha Query result."
class _CompleteQuery(dex.Step[QueryResult[GetCompanyProfileOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetCompanyProfileQuery"

    def execute(self, context: dex.Context, input: QueryResult[GetCompanyProfileOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TianyanchaGetCompanyProfileFlow(dex.Flow[GetCompanyProfileInput]):
    """Run the Tianyancha Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Tianyancha Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_get_company_profile_step(
            step_type="GetCompanyProfileQuery",
            step_input_type=GetCompanyProfileInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Tianyancha",
                "explanation": "Call Tianyancha GetCompanyProfile using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "TianyanchaGetCompanyProfile"

    def get_steps(self) -> dex.StepList[GetCompanyProfileInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
