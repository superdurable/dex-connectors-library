# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListAccountsInput selects an offset-based page of authorized accounts.
@dataclass(frozen=True, slots=True)
class ListAccountsInput:
    # Offset is the zero-based number of accounts to skip; zero starts at the first account.
    offset: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Limit is the maximum account count; zero uses the provider default of 100.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListAccountsInput, '__module__', 'dex_connectors.aweber.models')


# Account contains the stable account ID and display company.
@dataclass(frozen=True, slots=True)
class Account:
    # ID is the AWeber account's numeric identifier.
    id: str = ''
    # Company is the customer account's display name.
    company: str = ''


setattr(Account, '__module__', 'dex_connectors.aweber.models')


# AccountsPage is one offset-based account page.
@dataclass(frozen=True, slots=True)
class AccountsPage:
    # Accounts contains the visible customer accounts.
    accounts: tuple[Account, ...] = ()
    # Offset is the zero-based offset used for this request.
    offset: int = 0
    # Limit is the maximum account count used for this request.
    limit: int = 0
    # Total is the provider's full account count.
    total: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'accounts', _replace_nil_slices(self.accounts, 1))


AccountsPage.__annotations__['accounts'] = tuple[Account, ...] | None
AccountsPage.__dataclass_fields__['accounts'].type = tuple[Account, ...] | None
setattr(AccountsPage, '__module__', 'dex_connectors.aweber.models')
