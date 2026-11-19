# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read zhihu through one durable provider Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zhihu import Connection, ListQuotasInput, QuotaPage, new_list_quotas_step

CONNECTION_NAME = "zhihu"
_OUTCOME = dex.Attribute("zhihu-list_quotas-outcome", QuotaPage)


def map_to_operation_input(input: ListQuotasInput) -> ListQuotasInput:
    """Forward the typed Flow input to the provider Query."""
    return input


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Begin the zhihu read with the configured connection."
class _Start(dex.Step[ListQuotasInput]):
    def get_step_type(self) -> str:
        return "StartListQuotas"

    def execute(self, context: dex.Context, input: ListQuotasInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListQuotasQuery"), input)


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[QuotaPage]]):
    def get_step_type(self) -> str:
        return "CompleteListQuotas"

    def execute(self, context: dex.Context, input: QueryResult[QuotaPage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListQuotasFlow(dex.Flow[ListQuotasInput]):
    """Durably read zhihu with its static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("zhihu Flow requires its static connection")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_list_quotas_step(
            step_type="ListQuotasQuery",
            step_input_type=ListQuotasInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "zhihu",
                "explanation": "Call zhihu listQuotas.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "ZhihuListQuotas"

    def get_steps(self) -> dex.StepList[ListQuotasInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
