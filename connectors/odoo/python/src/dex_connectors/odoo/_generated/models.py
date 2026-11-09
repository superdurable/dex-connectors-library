# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SearchPartnersInput selects an Odoo ilike name pattern and result bound.
@dataclass(frozen=True, slots=True)
class SearchPartnersInput:
    # NamePattern is an Odoo ilike pattern such as %Acme%.
    name_pattern: str = _dataclass_field(default='', metadata={'dex_json_name': 'namePattern'})
    # Limit is one through one hundred; zero uses twenty.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(SearchPartnersInput, '__module__', 'dex_connectors.odoo.models')


# Partner is one matching company partner's public identity.
@dataclass(frozen=True, slots=True)
class Partner:
    # ID is Odoo's numeric partner ID.
    id: int = 0
    # Name is the company partner's display name.
    name: str = ''


setattr(Partner, '__module__', 'dex_connectors.odoo.models')


# PartnerSearchPage contains a bounded set of company partners.
@dataclass(frozen=True, slots=True)
class PartnerSearchPage:
    # Partners are matching companies in Odoo's default search order.
    partners: tuple[Partner, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'partners', _replace_nil_slices(self.partners, 1))


PartnerSearchPage.__annotations__['partners'] = tuple[Partner, ...] | None
PartnerSearchPage.__dataclass_fields__['partners'].type = tuple[Partner, ...] | None
setattr(PartnerSearchPage, '__module__', 'dex_connectors.odoo.models')
