# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetTransactionInput selects one Braintree GraphQL node by ID.
@dataclass(frozen=True, slots=True)
class GetTransactionInput:
    # TransactionID is the GraphQL transaction ID, not a legacy SDK ID.
    transaction_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'transactionId'})


setattr(GetTransactionInput, '__module__', 'dex_connectors.braintree.models')


# TransactionProfile contains a transaction's ID and current payment status.
@dataclass(frozen=True, slots=True)
class TransactionProfile:
    # ID is the Braintree GraphQL transaction ID.
    id: str = ''
    # Status is the Braintree PaymentStatus enum value.
    status: str = ''


setattr(TransactionProfile, '__module__', 'dex_connectors.braintree.models')
