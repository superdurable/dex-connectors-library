# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Flow boundary for a Zoho CRM record lookup."""

from dataclasses import dataclass, field

CONNECTION_NAME = "zoho-crm-main"


@dataclass(frozen=True, slots=True)
class RecordLookupInput:
    """Identify a Zoho CRM record to read."""

    module: str
    record_id: str = field(metadata={"dex_json_name": "recordId"})


@dataclass(frozen=True, slots=True)
class RecordLookupOutput:
    """Return the confirmed CRM record identity."""

    module: str
    record_id: str = field(metadata={"dex_json_name": "recordId"})
