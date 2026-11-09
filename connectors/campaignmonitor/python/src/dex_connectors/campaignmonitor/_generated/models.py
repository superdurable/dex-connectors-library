# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListClientsInput selects the account's accessible clients without filters.
@dataclass(frozen=True, slots=True)
class ListClientsInput:
    pass


setattr(ListClientsInput, '__module__', 'dex_connectors.campaignmonitor.models')


# ClientMetadata identifies one Campaign Monitor client without secrets.
@dataclass(frozen=True, slots=True)
class ClientMetadata:
    # ID is the provider's client identifier.
    id: str = ''
    # Name is the client display name.
    name: str = ''


setattr(ClientMetadata, '__module__', 'dex_connectors.campaignmonitor.models')


# ClientsList contains all returned client identities.
@dataclass(frozen=True, slots=True)
class ClientsList:
    # Clients is the account's accessible client list.
    clients: tuple[ClientMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'clients', _replace_nil_slices(self.clients, 1))


ClientsList.__annotations__['clients'] = tuple[ClientMetadata, ...] | None
ClientsList.__dataclass_fields__['clients'].type = tuple[ClientMetadata, ...] | None
setattr(ClientsList, '__module__', 'dex_connectors.campaignmonitor.models')
