# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Brevo contacts."""

from dataclasses import dataclass, field

from ._generated.models import Contact as Contact
from ._generated.models import CreateOrUpdateContactInput as CreateOrUpdateContactInput
from ._generated.models import GetContactInput as GetContactInput


@dataclass(frozen=True, slots=True)
class ContactWriteResult:
    """Saved contact email, whether Brevo created it, and the new ID when created."""

    email: str = ""
    created: bool = False
    id: int = field(default=0, metadata={"dex_json_omit_default": True})


CreateOrUpdateContactInput.__doc__ = (
    "Plain email address and uppercase Brevo attribute names, such as FNAME, with text values."
)
GetContactInput.__doc__ = "Plain email address of the contact."
Contact.__doc__ = "Brevo contact ID and email."
