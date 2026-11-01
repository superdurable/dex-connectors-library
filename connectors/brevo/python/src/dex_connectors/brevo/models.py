# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Brevo contacts."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class CreateOrUpdateContactInput:
    """Plain email address and uppercase Brevo attribute names, such as FNAME, with text values."""

    email: str = ""
    attributes: dict[str, str] = field(default_factory=dict, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class ContactWriteResult:
    """Saved contact email, whether Brevo created it, and the new ID when created."""

    email: str = ""
    created: bool = False
    id: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class GetContactInput:
    """Plain email address of the contact."""

    email: str = ""


@dataclass(frozen=True, slots=True)
class Contact:
    """Brevo contact ID and email."""

    id: int = 0
    email: str = ""
