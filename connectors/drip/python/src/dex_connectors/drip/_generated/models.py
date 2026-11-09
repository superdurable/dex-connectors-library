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


# ListAccountsInput selects accounts accessible to the connected Drip user.
@dataclass(frozen=True, slots=True)
class ListAccountsInput:
    pass


setattr(ListAccountsInput, '__module__', 'dex_connectors.drip.models')


# AccountMetadata identifies one Drip account without private settings.
@dataclass(frozen=True, slots=True)
class AccountMetadata:
    # ID is the stable Drip account identifier.
    id: str = ''
    # Name is the account display name.
    name: str = ''


setattr(AccountMetadata, '__module__', 'dex_connectors.drip.models')


# AccountsList contains accessible account identities.
@dataclass(frozen=True, slots=True)
class AccountsList:
    # Accounts contains only IDs and display names.
    accounts: tuple[AccountMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'accounts', _replace_nil_slices(self.accounts, 1))


AccountsList.__annotations__['accounts'] = tuple[AccountMetadata, ...] | None
AccountsList.__dataclass_fields__['accounts'].type = tuple[AccountMetadata, ...] | None
setattr(AccountsList, '__module__', 'dex_connectors.drip.models')


# GetSubscriberInput selects one subscriber by ID or email in an account.
@dataclass(frozen=True, slots=True)
class GetSubscriberInput:
    # AccountID is a numeric ID returned by ListAccounts.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    # SubscriberIDOrEmail is the Drip subscriber ID or email address.
    subscriber_id_or_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'subscriberIdOrEmail'})


setattr(GetSubscriberInput, '__module__', 'dex_connectors.drip.models')


# SubscriberMetadata contains subscriber identity and status without custom fields.
@dataclass(frozen=True, slots=True)
class SubscriberMetadata:
    # ID is the stable Drip subscriber identifier.
    id: str = ''
    # Email is the subscriber email address.
    email: str = ''
    # FirstName is the given name when present.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # LastName is the surname when present.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName'})
    # Status is the current subscriber status.
    status: str = ''


setattr(SubscriberMetadata, '__module__', 'dex_connectors.drip.models')
