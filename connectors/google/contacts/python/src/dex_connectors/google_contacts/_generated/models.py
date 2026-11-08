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


# ListContactsInput selects one page of the connected user's contacts.
@dataclass(frozen=True, slots=True)
class ListContactsInput:
    # PageSize is 1 through 1000; zero uses 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})
    # PageToken is Google's opaque cursor; blank selects the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken'})


setattr(ListContactsInput, '__module__', 'dex_connectors.google_contacts.models')


# Contact projects a People API resource name and its first display name and email address.
@dataclass(frozen=True, slots=True)
class Contact:
    # ResourceName is a stable People API identity such as people/c123.
    resource_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'resourceName'})
    # DisplayName is the first name returned with the personFields mask, if any.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName'})
    # EmailAddress is the first email address returned with the personFields mask, if any.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})


setattr(Contact, '__module__', 'dex_connectors.google_contacts.models')


# ContactsPage contains one bounded page and the next cursor.
@dataclass(frozen=True, slots=True)
class ContactsPage:
    # Contacts are projected contact identities.
    contacts: tuple[Contact, ...] = ()
    # NextPageToken is blank on the final page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'contacts', _replace_nil_slices(self.contacts, 1))


ContactsPage.__annotations__['contacts'] = tuple[Contact, ...] | None
ContactsPage.__dataclass_fields__['contacts'].type = tuple[Contact, ...] | None
setattr(ContactsPage, '__module__', 'dex_connectors.google_contacts.models')


# GetContactInput selects a contact by People API resource name.
@dataclass(frozen=True, slots=True)
class GetContactInput:
    # ResourceName is the people/{id} identity returned by listContacts.
    resource_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'resourceName'})


setattr(GetContactInput, '__module__', 'dex_connectors.google_contacts.models')


# CreateContactInput provides a name and optional email address for one contact.
@dataclass(frozen=True, slots=True)
class CreateContactInput:
    # GivenName is a nonempty contact given name.
    given_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'givenName'})
    # FamilyName is an optional contact family name.
    family_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'familyName'})
    # EmailAddress is an optional email address; blank omits it.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})


setattr(CreateContactInput, '__module__', 'dex_connectors.google_contacts.models')
