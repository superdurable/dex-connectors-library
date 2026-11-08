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


# GetAccountsInput selects a linked Item by its non-secret Plaid ID.
@dataclass(frozen=True, slots=True)
class GetAccountsInput:
    # ItemID is the Item ID returned by Plaid Link token exchange.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})


setattr(GetAccountsInput, '__module__', 'dex_connectors.plaid.models')


# AccountSummary contains only routing fields for one linked account.
@dataclass(frozen=True, slots=True)
class AccountSummary:
    # ID is the Plaid account ID.
    id: str = ''
    # Type is the broad account category.
    type: str = ''
    # Subtype is the narrower category when supplied by Plaid.
    subtype: str = ''


setattr(AccountSummary, '__module__', 'dex_connectors.plaid.models')


# AccountsSnapshot excludes names, masks, balances, and credentials.
@dataclass(frozen=True, slots=True)
class AccountsSnapshot:
    # ItemID identifies the linked Item.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})
    # Accounts contains the active account routing summaries.
    accounts: tuple[AccountSummary, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'accounts', _replace_nil_slices(self.accounts, 1))


AccountsSnapshot.__annotations__['accounts'] = tuple[AccountSummary, ...] | None
AccountsSnapshot.__dataclass_fields__['accounts'].type = tuple[AccountSummary, ...] | None
setattr(AccountsSnapshot, '__module__', 'dex_connectors.plaid.models')
