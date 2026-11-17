# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# CountClassObjectsInput identifies one existing Bmob class.
@dataclass(frozen=True, slots=True)
class CountClassObjectsInput:
    # ClassName is the case-sensitive Bmob class name from the app's Data console.
    class_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'className'})


setattr(CountClassObjectsInput, '__module__', 'dex_connectors.bmob.models')


# CountClassObjectsOutput contains the count without object fields or identifiers.
@dataclass(frozen=True, slots=True)
class CountClassObjectsOutput:
    # Count is the number of objects matching the class, as returned by Bmob.
    count: int = 0


setattr(CountClassObjectsOutput, '__module__', 'dex_connectors.bmob.models')
