# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetTransferInput selects a transfer by its non-secret Wise ID.
@dataclass(frozen=True, slots=True)
class GetTransferInput:
    # TransferID is the ID shown in Wise Transfers or returned by the API.
    transfer_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'transferId'})


setattr(GetTransferInput, '__module__', 'dex_connectors.wise.models')


# TransferStatus excludes recipient, customer, and payment details.
@dataclass(frozen=True, slots=True)
class TransferStatus:
    # ID is the Wise transfer ID.
    id: str = ''
    # SourceCurrency is the source ISO 4217 currency code.
    source_currency: str = _dataclass_field(default='', metadata={'dex_json_name': 'sourceCurrency'})
    # Status is the transfer workflow state such as incoming_payment_waiting.
    status: str = ''


setattr(TransferStatus, '__module__', 'dex_connectors.wise.models')
