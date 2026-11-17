# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read and persist the connected Chuanglan account's remaining SMS messages."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.chuanglan import (
    Connection,
    GetBalanceInput,
    GetBalanceOutput,
    new_get_balance_step,
)

CONNECTION_NAME = "chuanglan"
remaining_messages_attribute = dex.Attribute("chuanglan-remaining-messages-python", int)


# dex:group group-id:balance group-label:"SMS balance"
# dex:explanation text:"Record the fetched remaining message count and complete the Flow."
class RecordBalance(dex.Step[QueryResult[GetBalanceOutput]]):
    """Persist the fetched remaining count and return it as the Flow output."""

    def get_step_type(self) -> str:
        """Keep the recording Step identity stable."""
        return "RecordChuanglanBalance"

    def execute(self, context: dex.Context, input: QueryResult[GetBalanceOutput]) -> dex.StepDecision:
        """Record the non-secret remaining count."""
        remaining_messages_attribute.set(context, input.value.remaining_messages)
        return dex.graceful_complete(input.value)


class BalanceFlow(dex.Flow[GetBalanceInput]):
    """Read one Chuanglan SMS balance through a static named connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query and completion Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("balance Flow requires the static Chuanglan connection")
        self.record = RecordBalance()
        self.read = new_get_balance_step(
            step_type="ReadChuanglanBalance",
            step_input_type=GetBalanceInput,
            annotations={
                "groupId": "balance",
                "groupLabel": "SMS balance",
                "explanation": "POST the read-only balance query to the connected account node.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            fetched=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the durable Flow identity."""
        return "ChuanglanBalancePython"

    def get_steps(self) -> dex.StepList[GetBalanceInput]:
        """Register both provider and recording Steps."""
        return dex.StepList.start_step(self.read).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the remaining-count Attribute."""
        return dex.PersistenceSchema.of(remaining_messages_attribute)
