# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

# GetKeyInput identifies one Redis string key.
@dataclass(frozen=True, slots=True)
class GetKeyInput:
    # Key is the exact case-sensitive Redis key.
    key: str = ''


setattr(GetKeyInput, '__module__', 'dex_connectors.upstash_redis.models')


# KeyLookup contains the requested key and its string value when present.
@dataclass(frozen=True, slots=True)
class KeyLookup:
    # Key is the requested Redis key.
    key: str = ''
    # Value is empty when the key is missing or stores an empty string.
    value: str = ''
    # Exists distinguishes a missing key from an empty string value.
    exists: bool = False


setattr(KeyLookup, '__module__', 'dex_connectors.upstash_redis.models')
