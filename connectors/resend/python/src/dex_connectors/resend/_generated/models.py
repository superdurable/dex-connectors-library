# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return value
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SendEmailInput is the exact payload sent once per keyed email operation.
@dataclass(frozen=True, slots=True)
class SendEmailInput:
    # From is the verified sender email address.
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from'})
    # To contains recipient email addresses.
    to: tuple[str, ...] = ()
    # Subject is the email subject.
    subject: str = ''
    # HTML is the HTML email body.
    html: str = ''

    def __post_init__(self) -> None:
        object.__setattr__(self, 'to', _replace_nil_slices(self.to, 1))


SendEmailInput.__annotations__['to'] = tuple[str, ...] | None
setattr(SendEmailInput, '__module__', 'dex_connectors.resend.models')


# SentEmail identifies a queued email.
@dataclass(frozen=True, slots=True)
class SentEmail:
    # ID is the provider's stable resource identifier.
    id: str = ''


setattr(SentEmail, '__module__', 'dex_connectors.resend.models')


# GetEmailInput identifies one sent email.
@dataclass(frozen=True, slots=True)
class GetEmailInput:
    # ID is the provider's stable resource identifier.
    id: str = ''


setattr(GetEmailInput, '__module__', 'dex_connectors.resend.models')


# Email contains selected sent email fields.
@dataclass(frozen=True, slots=True)
class Email:
    # ID is the provider's stable resource identifier.
    id: str = ''
    # From is the verified sender email address.
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from'})
    # To contains recipient email addresses.
    to: tuple[str, ...] = ()
    # Subject is the email subject.
    subject: str = ''
    # CreatedAt is the provider's creation timestamp.
    created_at: str = ''

    def __post_init__(self) -> None:
        object.__setattr__(self, 'to', _replace_nil_slices(self.to, 1))


Email.__annotations__['to'] = tuple[str, ...] | None
setattr(Email, '__module__', 'dex_connectors.resend.models')
