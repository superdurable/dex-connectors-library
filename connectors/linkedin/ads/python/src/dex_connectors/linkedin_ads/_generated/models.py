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


# ListAdAccountsInput selects a cursor-based page of LinkedIn ad accounts.
@dataclass(frozen=True, slots=True)
class ListAdAccountsInput:
    # PageSize accepts 1 through 1000 accounts; zero uses 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is the opaque cursor from a previous page; blank starts at the beginning.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListAdAccountsInput, '__module__', 'dex_connectors.linkedin_ads.models')


# AdAccount contains a stable LinkedIn ad account identifier and name.
@dataclass(frozen=True, slots=True)
class AdAccount:
    # ID is the decimal account ID returned by LinkedIn.
    id: str = ''
    # Name is the account's display name.
    name: str = ''


setattr(AdAccount, '__module__', 'dex_connectors.linkedin_ads.models')


# AdAccountsPage contains one bounded page and its next cursor.
@dataclass(frozen=True, slots=True)
class AdAccountsPage:
    # Accounts are the visible account identities.
    accounts: tuple[AdAccount, ...] = ()
    # NextPageToken is blank when LinkedIn reported no further page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'accounts', _replace_nil_slices(self.accounts, 1))


AdAccountsPage.__annotations__['accounts'] = tuple[AdAccount, ...] | None
AdAccountsPage.__dataclass_fields__['accounts'].type = tuple[AdAccount, ...] | None
setattr(AdAccountsPage, '__module__', 'dex_connectors.linkedin_ads.models')
