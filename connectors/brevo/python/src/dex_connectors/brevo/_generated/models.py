# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# CreateOrUpdateContactInput supplies an email and absolute attribute values.
@dataclass(frozen=True, slots=True)
class CreateOrUpdateContactInput:
    # Email identifies the contact and must be a plain email address.
    email: str = ''
    # Attributes sets existing Brevo contact attributes by their uppercase names.
    attributes: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(CreateOrUpdateContactInput, '__module__', 'dex_connectors.brevo.models')


# ContactWriteResult confirms a contact create or update without exposing provider data.
@dataclass(frozen=True, slots=True)
class ContactWriteResult:
    # ID is available when Brevo created the contact.
    id: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Email is the submitted contact email.
    email: str = ''
    # Created distinguishes a 201 create from a 204 update.
    created: bool = False


setattr(ContactWriteResult, '__module__', 'dex_connectors.brevo.models')


# GetContactInput identifies a contact by email.
@dataclass(frozen=True, slots=True)
class GetContactInput:
    # Email is a plain email address.
    email: str = ''


setattr(GetContactInput, '__module__', 'dex_connectors.brevo.models')


# Contact contains the minimal non-secret contact identity returned by Brevo.
@dataclass(frozen=True, slots=True)
class Contact:
    # ID is Brevo's contact identifier.
    id: int = 0
    # Email is the contact email.
    email: str = ''


setattr(Contact, '__module__', 'dex_connectors.brevo.models')
