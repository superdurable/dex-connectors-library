# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List one page of Caoliao QR codes in a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.caoliao import (
    Connection,
    ListQRCodesInput,
    ListQRCodesOutput,
    new_list_qr_codes_step,
)

CONNECTION_NAME = "caoliao"
_OUTCOME = dex.Attribute("caoliao-qrcode-directory-outcome", ListQRCodesOutput)


def map_to_operation_input(input: ListQRCodesInput) -> ListQRCodesInput:
    """Pass the page request to the provider Query."""
    return input


# dex:group group-id:directory group-label:"QR-code directory"
# dex:explanation text:"Start listing QR-code names for the requested page."
class _StartDirectory(dex.Step[ListQRCodesInput]):
    def get_step_type(self) -> str:
        return "StartQRCodeDirectory"

    def execute(self, context: dex.Context, input: ListQRCodesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListQRCodesQuery"), input)


# dex:group group-id:directory group-label:"QR-code directory"
# dex:explanation text:"Persist the QR-code page and complete the Flow."
class _CompleteDirectory(dex.Step[QueryResult[ListQRCodesOutput]]):
    def get_step_type(self) -> str:
        return "CompleteQRCodeDirectory"

    def execute(self, context: dex.Context, input: QueryResult[ListQRCodesOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class QRCodeDirectoryFlow(dex.Flow[ListQRCodesInput]):
    """Durably list QR-code metadata using the static Caoliao connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("QR-code list requires the static Caoliao connection")
        self._start = _StartDirectory()
        self._complete = _CompleteDirectory()
        self._query = new_list_qr_codes_step(
            step_type="ListQRCodesQuery",
            step_input_type=ListQRCodesInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Caoliao",
                "explanation": "Call Caoliao listQRCodes with the configured OpenAPI Key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "CaoliaoQRCodeCatalog"

    def get_steps(self) -> dex.StepList[ListQRCodesInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable QR-code page Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
