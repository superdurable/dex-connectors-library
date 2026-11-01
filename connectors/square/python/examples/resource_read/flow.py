# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Square resource read Flow using the listPayments Query."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.square import Connection, ListPaymentsInput, PaymentPage, new_list_payments_step

FLOW_TYPE = "SquareResourceRead"
CONNECTION_NAME = "square"
_READ_STEP_TYPE = "ReadSquareResource"


@dataclass(frozen=True, slots=True)
class ResourceReadRequest:
    """Payment page cursor; blank reads the first page."""

    cursor: str = ""


@dataclass(frozen=True, slots=True)
class ResourceReadOutcome:
    """The payment page Square returned."""

    value: PaymentPage = field(default_factory=PaymentPage)


_OUTCOME = dex.Attribute("square-resource-read-outcome", ResourceReadOutcome)


def map_to_operation_input(request: ResourceReadRequest) -> ListPaymentsInput:
    """Read the payment page at the requested cursor."""
    return ListPaymentsInput(request.cursor)


# dex:group group-id:resource-read group-label:"Resource read"
# dex:explanation text:"Validate the request before reading the provider."
class _PrepareRequest(dex.Step[ResourceReadRequest]):
    def get_step_type(self) -> str:
        return "PrepareResourceRead"

    def execute(self, context: dex.Context, input: ResourceReadRequest) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_READ_STEP_TYPE), input)


# dex:group group-id:resource-read group-label:"Resource read"
# dex:explanation text:"Record the payment page and complete the Flow."
class _CompleteRead(dex.Step[QueryResult[PaymentPage]]):
    def get_step_type(self) -> str:
        return "CompleteResourceRead"

    def execute(self, context: dex.Context, input: QueryResult[PaymentPage]) -> dex.StepDecision:
        outcome = ResourceReadOutcome(input.value)
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class ResourceReadFlow(dex.Flow[ResourceReadRequest]):
    """Read one page of Square payments through the static square connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("resource read requires the static square connection")
        self._prepare = _PrepareRequest()
        self._complete = _CompleteRead()
        self._read = new_list_payments_step(
            step_type=_READ_STEP_TYPE,
            step_input_type=ResourceReadRequest,
            annotations={
                "groupId": "square",
                "groupLabel": "Square",
                "explanation": "Read the requested Square resource.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ResourceReadRequest]:
        """Register request preparation, the Square read, and completion Steps."""
        return dex.StepList.start_step(self._prepare).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the payment page outcome."""
        return dex.PersistenceSchema.of(_OUTCOME)
