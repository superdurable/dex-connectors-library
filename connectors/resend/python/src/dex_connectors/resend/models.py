# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Resend email inputs and provider records."""

from dataclasses import dataclass, field

from ._generated.models import GetEmailInput as GetEmailInput
from ._generated.models import SentEmail as SentEmail


@dataclass(frozen=True, slots=True)
class SendEmailInput:
    """One HTML email; the sender address must be on a domain verified in Resend."""

    from_address: str = field(default="", metadata={"dex_json_name": "from"})
    to: tuple[str, ...] = ()
    subject: str = ""
    html: str = ""


@dataclass(frozen=True, slots=True)
class Email:
    """Stored Resend email summary; created_at is Resend's timestamp text."""

    id: str = ""
    from_address: str = field(default="", metadata={"dex_json_name": "from"})
    to: tuple[str, ...] = ()
    subject: str = ""
    created_at: str = ""


SentEmail.__doc__ = "Resend's identifier for an accepted email."
GetEmailInput.__doc__ = "Resend email identifier returned by sendEmail."
