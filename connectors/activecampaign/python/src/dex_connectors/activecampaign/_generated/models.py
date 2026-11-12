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


# ListContactsInput selects an offset-based page of at most 100 contacts.
@dataclass(frozen=True, slots=True)
class ListContactsInput:
    # Offset is the zero-based number of contacts to skip; zero starts at the first contact.
    offset: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Limit is the maximum contact count; zero defaults to 20.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListContactsInput, '__module__', 'dex_connectors.activecampaign.models')


# ContactMetadata contains contact identity and name without embedded relationships.
@dataclass(frozen=True, slots=True)
class ContactMetadata:
    # ID is the stable provider contact ID.
    id: str = ''
    # Email is the contact email address, if present.
    email: str = ''
    # FirstName is the contact given name, if present.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName'})
    # LastName is the contact family name, if present.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName'})


setattr(ContactMetadata, '__module__', 'dex_connectors.activecampaign.models')


# ContactsPage is one offset-based contact page.
@dataclass(frozen=True, slots=True)
class ContactsPage:
    # Contacts contains the selected contact records.
    contacts: tuple[ContactMetadata, ...] = ()
    # Offset is the requested zero-based offset.
    offset: int = 0
    # Limit is the requested maximum count.
    limit: int = 0
    # Total is the provider's matching contact count.
    total: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'contacts', _replace_nil_slices(self.contacts, 1))


ContactsPage.__annotations__['contacts'] = tuple[ContactMetadata, ...] | None
ContactsPage.__dataclass_fields__['contacts'].type = tuple[ContactMetadata, ...] | None
setattr(ContactsPage, '__module__', 'dex_connectors.activecampaign.models')


# GetContactInput selects one numeric ActiveCampaign contact ID.
@dataclass(frozen=True, slots=True)
class GetContactInput:
    # ContactID is the numeric ID from the Contacts page or API.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})


setattr(GetContactInput, '__module__', 'dex_connectors.activecampaign.models')
