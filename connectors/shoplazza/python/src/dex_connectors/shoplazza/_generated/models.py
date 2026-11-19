# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

# GetShopInput identifies the configured store; it needs no additional fields.
@dataclass(frozen=True, slots=True)
class GetShopInput:
    pass


setattr(GetShopInput, '__module__', 'dex_connectors.shoplazza.models')


# GetShopOutput contains non-contact store metadata only.
@dataclass(frozen=True, slots=True)
class GetShopOutput:
    # ID is Shoplazza's stable numeric store identifier.
    id: int = 0
    # Name is the store's display name.
    name: str = ''
    # Domain is the store's public domain.
    domain: str = ''
    # Currency is the store's ISO currency code.
    currency: str = ''


setattr(GetShopOutput, '__module__', 'dex_connectors.shoplazza.models')
