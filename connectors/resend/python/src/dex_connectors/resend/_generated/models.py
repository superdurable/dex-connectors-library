# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class SendEmailInput:
    """Typed SendEmailInput value from the Go connector contract."""

    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from'})
    to: tuple[str, ...] | None = None
    subject: str = ''
    html: str = ''


setattr(SendEmailInput, '__module__', 'dex_connectors.resend.models')


@dataclass(frozen=True, slots=True)
class SentEmail:
    """Typed SentEmail value from the Go connector contract."""

    id: str = ''


setattr(SentEmail, '__module__', 'dex_connectors.resend.models')


@dataclass(frozen=True, slots=True)
class GetEmailInput:
    """Typed GetEmailInput value from the Go connector contract."""

    id: str = ''


setattr(GetEmailInput, '__module__', 'dex_connectors.resend.models')


@dataclass(frozen=True, slots=True)
class Email:
    """Typed Email value from the Go connector contract."""

    id: str = ''
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from'})
    to: tuple[str, ...] | None = None
    subject: str = ''
    created_at: str = ''


setattr(Email, '__module__', 'dex_connectors.resend.models')
