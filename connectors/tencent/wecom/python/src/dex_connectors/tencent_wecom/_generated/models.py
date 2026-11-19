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


# SendGroupRobotMessageInput is one post to the group chat that owns the connection's webhook key.
@dataclass(frozen=True, slots=True)
class SendGroupRobotMessageInput:
    # MessageType is text or markdown.
    message_type: str = _dataclass_field(metadata={'dex_json_name': 'messageType'})
    # Content is the UTF-8 message body: at most 2048 bytes for text and 4096 bytes for markdown.
    content: str = ''
    # MentionedUserIDs lists WeCom member user IDs to mention in a text post; "@all" mentions everyone. Markdown posts cannot mention.
    mentioned_user_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'mentionedUserIds', 'dex_json_omit_default': True})
    # MentionedMobileNumbers lists member mobile numbers to mention in a text post; "@all" mentions everyone. Markdown posts cannot mention.
    mentioned_mobile_numbers: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'mentionedMobileNumbers', 'dex_json_omit_default': True})


setattr(SendGroupRobotMessageInput, '__module__', 'dex_connectors.tencent_wecom.models')


# SentGroupRobotMessage confirms a post WeCom accepted. It never repeats the posted content.
@dataclass(frozen=True, slots=True)
class SentGroupRobotMessage:
    # MessageType is the posted message type.
    message_type: str = _dataclass_field(metadata={'dex_json_name': 'messageType'})
    # ContentBytes is the UTF-8 byte length of the posted content.
    content_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'contentBytes'})


setattr(SentGroupRobotMessage, '__module__', 'dex_connectors.tencent_wecom.models')


# SendApplicationMessageInput is one message from the connection's self-built application.
# Choose ToAllMembers, or at least one of ToUserIDs, ToDepartmentIDs, and ToTagIDs.
@dataclass(frozen=True, slots=True)
class SendApplicationMessageInput:
    # ToAllMembers sends to every member in the application's visible range and excludes the ID lists.
    to_all_members: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'toAllMembers', 'dex_json_omit_default': True})
    # ToUserIDs lists at most 1000 member user IDs, as shown in the admin console Contacts > member > Account.
    to_user_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'toUserIds', 'dex_json_omit_default': True})
    # ToDepartmentIDs lists at most 100 decimal department IDs.
    to_department_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'toDepartmentIds', 'dex_json_omit_default': True})
    # ToTagIDs lists at most 100 decimal tag IDs.
    to_tag_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'toTagIds', 'dex_json_omit_default': True})
    # MessageType is text or markdown.
    message_type: str = _dataclass_field(kw_only=True, metadata={'dex_json_name': 'messageType'})
    # Content is the UTF-8 message body of at most 2048 bytes.
    content: str = ''
    # IsConfidential marks a text message as confidential: WeCom blocks forwarding and adds a watermark. Markdown messages cannot set it.
    is_confidential: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isConfidential', 'dex_json_omit_default': True})


setattr(SendApplicationMessageInput, '__module__', 'dex_connectors.tencent_wecom.models')


# SentApplicationMessage identifies an accepted application message and the recipients WeCom skipped.
@dataclass(frozen=True, slots=True)
class SentApplicationMessage:
    # MessageID is WeCom's msgid, usable to recall the message; empty only if WeCom omits it.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # InvalidUserIDs lists requested user IDs that do not exist or are outside the visible range, in lowercase.
    invalid_user_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'invalidUserIds'})
    # InvalidDepartmentIDs lists requested department IDs that do not exist or are outside the visible range.
    invalid_department_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'invalidDepartmentIds'})
    # InvalidTagIDs lists requested tag IDs that do not exist or are outside the visible range.
    invalid_tag_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'invalidTagIds'})
    # UnlicensedUserIDs lists recipients in the visible range without a WeCom basic interface license.
    unlicensed_user_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'unlicensedUserIds'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'invalid_user_ids', _replace_nil_slices(self.invalid_user_ids, 1))
        object.__setattr__(self, 'invalid_department_ids', _replace_nil_slices(self.invalid_department_ids, 1))
        object.__setattr__(self, 'invalid_tag_ids', _replace_nil_slices(self.invalid_tag_ids, 1))
        object.__setattr__(self, 'unlicensed_user_ids', _replace_nil_slices(self.unlicensed_user_ids, 1))


SentApplicationMessage.__annotations__['invalid_user_ids'] = tuple[str, ...] | None
SentApplicationMessage.__dataclass_fields__['invalid_user_ids'].type = tuple[str, ...] | None
SentApplicationMessage.__annotations__['invalid_department_ids'] = tuple[str, ...] | None
SentApplicationMessage.__dataclass_fields__['invalid_department_ids'].type = tuple[str, ...] | None
SentApplicationMessage.__annotations__['invalid_tag_ids'] = tuple[str, ...] | None
SentApplicationMessage.__dataclass_fields__['invalid_tag_ids'].type = tuple[str, ...] | None
SentApplicationMessage.__annotations__['unlicensed_user_ids'] = tuple[str, ...] | None
SentApplicationMessage.__dataclass_fields__['unlicensed_user_ids'].type = tuple[str, ...] | None
setattr(SentApplicationMessage, '__module__', 'dex_connectors.tencent_wecom.models')
