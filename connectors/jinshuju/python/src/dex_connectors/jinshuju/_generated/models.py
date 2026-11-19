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


# ListFormsPageInput optionally continues from a previously returned page cursor.
@dataclass(frozen=True, slots=True)
class ListFormsPageInput:
    # Next is the opaque cursor returned by the previous page; blank starts at the first page.
    next: str = ''


setattr(ListFormsPageInput, '__module__', 'dex_connectors.jinshuju.models')


# FormMetadata identifies a form without its creator or submissions.
@dataclass(frozen=True, slots=True)
class FormMetadata:
    # Name is the form's display name.
    name: str = ''
    # Token is the form identifier used by other Jinshuju API paths.
    token: str = ''


setattr(FormMetadata, '__module__', 'dex_connectors.jinshuju.models')


# ListFormsPageOutput contains one page of safe form metadata.
@dataclass(frozen=True, slots=True)
class ListFormsPageOutput:
    # Total is the number of accessible forms.
    total: int = 0
    # Count is the number of forms in this page.
    count: int = 0
    # Forms are the accessible forms in this page.
    forms: tuple[FormMetadata, ...] = ()
    # Next is the opaque cursor for the next page; blank means no next page.
    next: str = ''

    def __post_init__(self) -> None:
        object.__setattr__(self, 'forms', _replace_nil_slices(self.forms, 1))


ListFormsPageOutput.__annotations__['forms'] = tuple[FormMetadata, ...] | None
ListFormsPageOutput.__dataclass_fields__['forms'].type = tuple[FormMetadata, ...] | None
setattr(ListFormsPageOutput, '__module__', 'dex_connectors.jinshuju.models')
