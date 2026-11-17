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


# SearchPoliciesInput contains a natural-language electricity policy query and bounded result count.
@dataclass(frozen=True, slots=True)
class SearchPoliciesInput:
    # Query is a nonempty natural-language electricity policy search.
    query: str = ''
    # TopK is the number of cards from 1 to 50; zero uses the provider's documented default of 10.
    top_k: int = _dataclass_field(default=0, metadata={'dex_json_name': 'topK', 'dex_json_omit_default': True})


setattr(SearchPoliciesInput, '__module__', 'dex_connectors.enlvon.models')


# PolicyCard contains source metadata without generated analysis or full document text.
@dataclass(frozen=True, slots=True)
class PolicyCard:
    # Title is the policy title.
    title: str = ''
    # Date is the provider's publication date string.
    date: str = ''
    # Source is the provider's source identifier.
    source: str = ''
    # OriginalURL is the provider's link to the original government document.
    original_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'originalURL'})


setattr(PolicyCard, '__module__', 'dex_connectors.enlvon.models')


# SearchPoliciesOutput contains policy cards that match the query.
@dataclass(frozen=True, slots=True)
class SearchPoliciesOutput:
    # Policies are the returned cards, possibly empty.
    policies: tuple[PolicyCard, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'policies', _replace_nil_slices(self.policies, 1))


SearchPoliciesOutput.__annotations__['policies'] = tuple[PolicyCard, ...] | None
SearchPoliciesOutput.__dataclass_fields__['policies'].type = tuple[PolicyCard, ...] | None
setattr(SearchPoliciesOutput, '__module__', 'dex_connectors.enlvon.models')
