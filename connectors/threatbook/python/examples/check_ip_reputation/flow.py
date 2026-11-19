# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable ThreatBook checkIPReputation Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.threatbook import (
    CheckIPReputationInput,
    CheckIPReputationOutput,
    Connection,
    new_check_ip_reputation_step,
)

CONNECTION_NAME = "threatbook"
_RESULT = dex.Attribute("threatbook-result", CheckIPReputationOutput)


def map_to_operation_input(request: CheckIPReputationInput) -> CheckIPReputationInput:
    """Forward the provider request."""
    return request


# dex:group group-id:threatbook group-label:"ThreatBook"
# dex:explanation text:"Start the ThreatBook query."
class _StartQuery(dex.Step[CheckIPReputationInput]):
    def execute(self, context: dex.Context, input: CheckIPReputationInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:threatbook group-label:"ThreatBook"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[CheckIPReputationOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[CheckIPReputationOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CheckIPReputationFlow(dex.Flow[CheckIPReputationInput]):
    """Run one ThreatBook query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ThreatBook Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_check_ip_reputation_step(
            step_type="CallProvider",
            step_input_type=CheckIPReputationInput,
            annotations={
                "groupId": "threatbook",
                "groupLabel": "ThreatBook",
                "explanation": "Run ThreatBook checkIPReputation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "CheckIPReputationThreatbook"

    def get_steps(self) -> dex.StepList[CheckIPReputationInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
