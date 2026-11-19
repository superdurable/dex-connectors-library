# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Submail provider data in a durable Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.submail import Connection, GetSMSBalanceInput, SMSBalance, new_get_sms_balance_step

CONNECTION_NAME = "submail"
_RESULT = dex.Attribute("submail-query-result", SMSBalance)


def map_to_operation_input(input: GetSMSBalanceInput) -> GetSMSBalanceInput:
    """Pass the application Query input to the provider."""
    return input


# dex:group group-id:submail group-label:"Submail"
# dex:explanation text:"Start the Submail Query."
class _StartQuery(dex.Step[GetSMSBalanceInput]):
    def get_step_type(self) -> str:
        return "StartSubmailQuery"

    def execute(self, context: dex.Context, input: GetSMSBalanceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallSubmail"), input)


# dex:group group-id:submail group-label:"Submail"
# dex:explanation text:"Persist the Submail Query result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[SMSBalance]]):
    def get_step_type(self) -> str:
        return "CompleteSubmailQuery"

    def execute(self, context: dex.Context, input: QueryResult[SMSBalance]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProviderQueryFlow(dex.Flow[GetSMSBalanceInput]):
    """Read the configured Submail account through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ProviderQueryFlow requires the static Submail connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_sms_balance_step(
            step_type="CallSubmail",
            step_input_type=GetSMSBalanceInput,
            annotations={
                "groupId": "submail",
                "groupLabel": "Submail",
                "explanation": "Read Submail account data.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Submail Query Flow identity."""
        return "SubmailProviderQuery"

    def get_steps(self) -> dex.StepList[GetSMSBalanceInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider Query output."""
        return dex.PersistenceSchema.of(_RESULT)
