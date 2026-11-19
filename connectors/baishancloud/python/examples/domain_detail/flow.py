# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one BaishanCloud provider result in a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.baishancloud import (
    Connection,
    DescribeDomainInput,
    DomainDetail,
    new_describe_domain_step,
)

CONNECTION_NAME = "baishancloud"
_RESULT = dex.Attribute("baishancloud-domain-detail", DomainDetail)


def map_to_operation_input(input: DescribeDomainInput) -> DescribeDomainInput:
    """Pass the public Flow input to the provider Query."""
    return input


# dex:group group-id:baishancloud group-label:"BaishanCloud"
# dex:explanation text:"Start the BaishanCloud Query for this Flow."
class _Start(dex.Step[DescribeDomainInput]):
    def get_step_type(self) -> str:
        return "StartDomainDetailFlow"

    def execute(self, context: dex.Context, input: DescribeDomainInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("DescribeDomainQuery"), input)


# dex:group group-id:baishancloud group-label:"BaishanCloud"
# dex:explanation text:"Persist the validated provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[DomainDetail]]):
    def get_step_type(self) -> str:
        return "CompleteDomainDetail"

    def execute(self, context: dex.Context, input: QueryResult[DomainDetail]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DomainDetailFlow(dex.Flow[DescribeDomainInput]):
    """Call BaishanCloud through the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("BaishanCloud connection name differs")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_describe_domain_step(
            step_type="DescribeDomainQuery",
            step_input_type=DescribeDomainInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "BaishanCloud",
                "explanation": "Read one configured BaishanCloud CDN domain.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable application Flow identity."""
        return "BaishanCloudDomainDetailFlow"

    def get_steps(self) -> dex.StepList[DescribeDomainInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
