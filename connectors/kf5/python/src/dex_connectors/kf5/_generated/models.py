# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetTicketInput identifies one ticket in the configured KF5 tenant.
@dataclass(frozen=True, slots=True)
class GetTicketInput:
    # TicketID is a positive KF5 ticket identifier.
    ticket_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'ticketID'})


setattr(GetTicketInput, '__module__', 'dex_connectors.kf5.models')


# GetTicketOutput contains only non-content ticket status metadata.
@dataclass(frozen=True, slots=True)
class GetTicketOutput:
    # TicketID is the KF5 ticket identifier.
    ticket_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'ticketID'})
    # Status is the provider's ticket status.
    status: str = ''
    # Priority is the provider's ticket priority when set.
    priority: str = ''
    # Type is the provider's ticket type when set.
    type: str = ''
    # CreatedAt is the provider's ticket creation timestamp.
    created_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdAt'})
    # UpdatedAt is the provider's latest ticket timestamp.
    updated_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'updatedAt'})


setattr(GetTicketOutput, '__module__', 'dex_connectors.kf5.models')
