# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Wire behavior for generated pointer and nullable slice models."""

import json
from dataclasses import dataclass, field

from dex import CodecRegistry, Value, WireKind

from dex_connectors.unsplash._generated.models import Photo, PhotoPage


@dataclass(frozen=True, slots=True)
class PointerPresenceWire:
    is_completed: bool | None = field(
        default=None, metadata={"dex_json_name": "isCompleted", "dex_json_omit_default": True}
    )
    wait_time_seconds: int | None = field(
        default=None, metadata={"dex_json_name": "waitTimeSeconds", "dex_json_omit_default": True}
    )
    items: tuple[str, ...] | None = field(default=None, metadata={"dex_json_omit_default": True})


def encoded_json(value: object) -> dict[str, object]:
    encoded = CodecRegistry().resolve(type(value)).encode(value)
    assert isinstance(encoded.data, str)
    result: dict[str, object] = json.loads(encoded.data)
    return result


def test_pointer_presence_wire_keeps_false_zero_and_empty_slice() -> None:
    codec = CodecRegistry().resolve(PointerPresenceWire)
    assert encoded_json(PointerPresenceWire()) == {}
    present = PointerPresenceWire(is_completed=False, wait_time_seconds=0, items=())
    assert encoded_json(present) == {
        "isCompleted": False,
        "waitTimeSeconds": 0,
        "items": [],
    }
    decoded = codec.decode(codec.encode(present))
    assert decoded.is_completed is False
    assert decoded.wait_time_seconds == 0
    assert decoded.items == []


def test_optional_pointer_omits_none_but_emits_present_empty_string() -> None:
    codec = CodecRegistry().resolve(Photo)
    absent = encoded_json(Photo(id="p1"))
    present = encoded_json(Photo(id="p1", description=""))
    assert "description" not in absent
    assert present["description"] == ""
    assert codec.decode(Value(WireKind.JSON, json.dumps(present))).description == ""


def test_nullable_slice_decodes_null() -> None:
    codec = CodecRegistry().resolve(PhotoPage)
    page = codec.decode(Value(WireKind.JSON, '{"total":0,"totalPages":0,"photos":null}'))
    assert page.photos is None
