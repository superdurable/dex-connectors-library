// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { ListedPost } from '../src/index.js';
import { mergeTopicPosts, selectTopicMentions } from '../examples/topic-posts/flow.js';

function listedPost(id: string, overrides: Partial<ListedPost> = {}): ListedPost {
  return {
    id, title: 'Profiling tips', subreddit: 'golang', author: 'example_author', permalink: `https://www.reddit.com/r/golang/comments/${id}/`,
    isSelf: true, score: 1, commentCount: 0, createdAt: '2026-11-10T09:00:00Z', isOver18: false, isSpoiler: false, isStickied: false, ...overrides,
  };
}

describe('topic posts example', () => {
  it('selects mentions ignoring case and pinned posts', () => {
    const mentions = selectTopicMentions([
      listedPost('t3_pinned', { title: 'Generics questions thread', isStickied: true }),
      listedPost('t3_title', { title: 'Why GENERICS?' }),
      listedPost('t3_body', { title: 'Type sets', selfText: 'How do generics constraints work?' }),
      listedPost('t3_other'),
    ], 'Generics');
    expect(mentions.map(post => post.id)).toEqual(['t3_title', 't3_body']);
  });

  it('deduplicates and orders newest first', () => {
    const older = '2026-11-10T09:00:00Z', newer = '2026-11-10T10:00:00Z';
    const merged = mergeTopicPosts(
      [listedPost('t3_b', { createdAt: older }), listedPost('t3_c', { createdAt: newer })],
      [listedPost('t3_c', { createdAt: newer }), listedPost('t3_pin', { createdAt: newer, isStickied: true }), listedPost('t3_a', { createdAt: older })],
    );
    expect(merged.map(post => post.id)).toEqual(['t3_c', 't3_a', 't3_b']);
    expect(mergeTopicPosts([], [])).toEqual([]);
  });
});
