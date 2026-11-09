# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetPostInput names a Reddit post by its full t3_ ID.
@dataclass(frozen=True, slots=True)
class GetPostInput:
    # PostID is the full t3_ ID copied from the Reddit post URL or API.
    post_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'postId'})


setattr(GetPostInput, '__module__', 'dex_connectors.reddit.models')


# Post contains bounded public metadata for one Reddit post.
@dataclass(frozen=True, slots=True)
class Post:
    # ID is the full t3_ post ID.
    id: str = ''
    # Title is the post's current title.
    title: str = ''
    # Subreddit is the community name without r/.
    subreddit: str = ''
    # Permalink is the relative Reddit post URL.
    permalink: str = ''
    # Score is the current vote score, which may change.
    score: int = 0


setattr(Post, '__module__', 'dex_connectors.reddit.models')
