# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetContactInput identifies one Keap CRM contact.
@dataclass(frozen=True, slots=True)
class GetContactInput:
    # ContactID is the exact Keap contact ID.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId'})


setattr(GetContactInput, '__module__', 'dex_connectors.keap.models')


# ContactProfile contains the selected contact's ID and optional names.
@dataclass(frozen=True, slots=True)
class ContactProfile:
    # ID is the Keap contact ID.
    id: str = ''
    # GivenName is the contact's first name when available.
    given_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'givenName'})
    # FamilyName is the contact's surname when available.
    family_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'familyName'})


setattr(ContactProfile, '__module__', 'dex_connectors.keap.models')
