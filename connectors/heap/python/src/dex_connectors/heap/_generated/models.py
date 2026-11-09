# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

# SetUserPropertiesInput identifies one user and a bounded set of non-secret string properties.
@dataclass(frozen=True, slots=True)
class SetUserPropertiesInput:
    # Identity is a stable, unique identity already used by Heap; it is case-sensitive.
    identity: str = ''
    # Properties are absolute replacement values. Do not pass secrets.
    properties: dict[str, str] | None = None


setattr(SetUserPropertiesInput, '__module__', 'dex_connectors.heap.models')


# UserPropertiesResult confirms Heap accepted the requested property values.
@dataclass(frozen=True, slots=True)
class UserPropertiesResult:
    # Accepted is true only after Heap returns HTTP 200.
    accepted: bool = False


setattr(UserPropertiesResult, '__module__', 'dex_connectors.heap.models')
