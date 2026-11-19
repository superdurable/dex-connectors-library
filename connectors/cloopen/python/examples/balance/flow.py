# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read and persist the connected Cloopen main account's decimal balance."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.cloopen import (
    Connection,
    GetBalanceInput,
    GetBalanceOutput,
    new_get_balance_step,
)

CONNECTION_NAME = "cloopen"
account_balance_attribute = dex.Attribute("cloopen-account-balance-python", str)


# dex:group group-id:balance group-label:"Account balance"
# dex:explanation text:"Record the fetched decimal balance and complete the Flow."
class RecordBalance(dex.Step[QueryResult[GetBalanceOutput]]):
    """Persist the fetched decimal balance and return it as the Flow output."""

    def get_step_type(self) -> str:
        """Keep the recording Step identity stable."""
        return "RecordCloopenBalance"

    def execute(self, context: dex.Context, input: QueryResult[GetBalanceOutput]) -> dex.StepDecision:
        """Record the non-secret decimal balance."""
        account_balance_attribute.set(context, input.value.balance)
        return dex.graceful_complete(input.value)


class BalanceFlow(dex.Flow[GetBalanceInput]):
    """Read one Cloopen main-account balance through a static named connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query and completion Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("balance Flow requires the static Cloopen connection")
        self.record = RecordBalance()
        self.read = new_get_balance_step(
            step_type="ReadCloopenBalance",
            step_input_type=GetBalanceInput,
            annotations={
                "groupId": "balance",
                "groupLabel": "Account balance",
                "explanation": "Sign and GET the read-only main-account balance.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            fetched=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the durable Flow identity."""
        return "CloopenBalancePython"

    def get_steps(self) -> dex.StepList[GetBalanceInput]:
        """Register both provider and recording Steps."""
        return dex.StepList.start_step(self.read).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the decimal balance Attribute."""
        return dex.PersistenceSchema.of(account_balance_attribute)
