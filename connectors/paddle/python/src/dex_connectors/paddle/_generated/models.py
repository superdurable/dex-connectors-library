# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetTransactionInput selects a transaction by its non-secret Paddle ID.
@dataclass(frozen=True, slots=True)
class GetTransactionInput:
    # TransactionID is a Paddle Billing transaction ID prefixed txn_.
    transaction_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'transactionId'})


setattr(GetTransactionInput, '__module__', 'dex_connectors.paddle.models')


# TransactionStatus excludes customer identities and payment details.
@dataclass(frozen=True, slots=True)
class TransactionStatus:
    # ID is the Paddle transaction ID.
    id: str = ''
    # Status is a Paddle transaction state such as completed.
    status: str = ''
    # CurrencyCode is the transaction's ISO 4217 currency.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode'})


setattr(TransactionStatus, '__module__', 'dex_connectors.paddle.models')
