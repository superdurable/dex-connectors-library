# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetPostInput selects one public X Post by its numeric URL ID.
@dataclass(frozen=True, slots=True)
class GetPostInput:
    # PostID is the decimal value after /status/ in an x.com Post URL.
    post_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'postId'})


setattr(GetPostInput, '__module__', 'dex_connectors.x_twitter.models')


# PostMetadata contains the public Post identity and text.
@dataclass(frozen=True, slots=True)
class PostMetadata:
    # ID is the decimal Post ID returned by X.
    id: str = ''
    # Text is the Post's public text.
    text: str = ''


setattr(PostMetadata, '__module__', 'dex_connectors.x_twitter.models')
