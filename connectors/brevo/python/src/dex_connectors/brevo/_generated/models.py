# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class CreateOrUpdateContactInput:
    """Typed CreateOrUpdateContactInput value from the Go connector contract."""

    email: str = ''
    attributes: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(CreateOrUpdateContactInput, '__module__', 'dex_connectors.brevo.models')


@dataclass(frozen=True, slots=True)
class ContactWriteResult:
    """Typed ContactWriteResult value from the Go connector contract."""

    id: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    email: str = ''
    created: bool = False


setattr(ContactWriteResult, '__module__', 'dex_connectors.brevo.models')


@dataclass(frozen=True, slots=True)
class GetContactInput:
    """Typed GetContactInput value from the Go connector contract."""

    email: str = ''


setattr(GetContactInput, '__module__', 'dex_connectors.brevo.models')


@dataclass(frozen=True, slots=True)
class Contact:
    """Typed Contact value from the Go connector contract."""

    id: int = 0
    email: str = ''


setattr(Contact, '__module__', 'dex_connectors.brevo.models')
