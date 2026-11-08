# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAccountInput selects a Recurly account by ID or code- prefixed account code.
@dataclass(frozen=True, slots=True)
class GetAccountInput:
    # AccountID is an account ID or a code- prefixed account code.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})


setattr(GetAccountInput, '__module__', 'dex_connectors.recurly.models')


# AccountIdentity excludes contact, billing, and payment details.
@dataclass(frozen=True, slots=True)
class AccountIdentity:
    # ID is the Recurly account ID.
    id: str = ''
    # Code is the account's stable merchant-supplied code.
    code: str = ''


setattr(AccountIdentity, '__module__', 'dex_connectors.recurly.models')
