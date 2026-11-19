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


# ListBucketsInput has no parameters; Qiniu lists buckets owned by the signing key.
@dataclass(frozen=True, slots=True)
class ListBucketsInput:
    pass


setattr(ListBucketsInput, '__module__', 'dex_connectors.qiniu.models')


# ListBucketsOutput contains the names of buckets owned by the signing account.
@dataclass(frozen=True, slots=True)
class ListBucketsOutput:
    # Buckets is empty when the account owns no buckets.
    buckets: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'buckets', _replace_nil_slices(self.buckets, 1))


ListBucketsOutput.__annotations__['buckets'] = tuple[str, ...] | None
ListBucketsOutput.__dataclass_fields__['buckets'].type = tuple[str, ...] | None
setattr(ListBucketsOutput, '__module__', 'dex_connectors.qiniu.models')
