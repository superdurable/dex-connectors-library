// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call, Failure, QueryAttempt, QueryDefinition } from '@superdurable/connectors-sdk';
import { branchCase, MockQuery, type Case } from '@superdurable/connectors-sdk/connectormock';
import { GeneratedMock as ConnectorMock, emptyItemSearchPage, emptyStorySearchPage } from './generated/mock.js';
import { SearchItemsDefinition, SearchStoriesDefinition } from './generated/definitions.js';
import type { ItemSearchPage, SearchItemsInput, SearchStoriesInput, StorySearchPage } from './generated/models.js';

export * from './generated/mock.js';


const publicItemsPages: ItemSearchPage[] = [
  {
    "items": [
      {
        "id": 42001,
        "type": "story",
        "title": "A compiler release",
        "author": "compilerdev",
        "createdAt": "2026-10-26T11:00:00Z",
        "discussionUrl": "https://news.ycombinator.com/item?id=42001"
      }
    ],
    "page": 0,
    "pages": 3,
    "totalHits": 3,
    "hitsPerPage": 1,
    "nextPage": 1
  },
  {
    "items": [
      {
        "id": 42002,
        "type": "comment",
        "text": "A useful compiler note",
        "author": "reviewer",
        "createdAt": "2026-10-26T11:01:00Z",
        "discussionUrl": "https://news.ycombinator.com/item?id=42002"
      }
    ],
    "page": 1,
    "pages": 3,
    "totalHits": 3,
    "hitsPerPage": 1,
    "nextPage": 2
  },
  {
    "items": [
      {
        "id": 42003,
        "type": "poll",
        "title": "Compiler poll",
        "author": "compilerdev",
        "createdAt": "2026-10-26T11:02:00Z",
        "discussionUrl": "https://news.ycombinator.com/item?id=42003"
      }
    ],
    "page": 2,
    "pages": 3,
    "totalHits": 3,
    "hitsPerPage": 1,
    "nextPage": 0
  }
];

/** Select one zero-based page from the manifest's publicItems scenario. */
export function searchItemsPublicItems(page = 0): Case<ItemSearchPage> {
  const selected = publicItemsPages[page];
  if (!selected) throw new RangeError('page outside the manifest mock');
  return branchCase('searched', structuredClone(selected));
}

/** Return the manifest's default item search page. */
export function searchItemsDefault(page = 0): Case<ItemSearchPage> { return searchItemsPublicItems(page); }

const compilerStoriesPages: StorySearchPage[] = [
  {
    "stories": [
      {
        "id": 42003,
        "title": "Show HN: A compiler explorer for small languages",
        "url": "https://example.com/explorer",
        "discussionUrl": "https://news.ycombinator.com/item?id=42003",
        "author": "langtools",
        "points": 12,
        "commentCount": 3,
        "createdAt": "2026-10-26T11:50:00Z",
        "tags": [
          "story",
          "show_hn"
        ]
      },
      {
        "id": 42001,
        "title": "A compiler release",
        "url": "https://example.com/compiler",
        "discussionUrl": "https://news.ycombinator.com/item?id=42001",
        "author": "compilerdev",
        "points": 100,
        "commentCount": 40,
        "createdAt": "2026-10-26T11:00:00Z",
        "tags": [
          "story",
          "front_page"
        ]
      }
    ],
    "page": 0,
    "pages": 3,
    "totalHits": 5,
    "hitsPerPage": 2,
    "nextPage": 1
  },
  {
    "stories": [
      {
        "id": 42001,
        "title": "A compiler release",
        "url": "https://example.com/compiler",
        "discussionUrl": "https://news.ycombinator.com/item?id=42001",
        "author": "compilerdev",
        "points": 100,
        "commentCount": 40,
        "createdAt": "2026-10-26T11:00:00Z",
        "tags": [
          "story",
          "front_page"
        ]
      },
      {
        "id": 41990,
        "title": "Ask HN: How do you test a compiler?",
        "discussionUrl": "https://news.ycombinator.com/item?id=41990",
        "author": "testwriter",
        "points": 8,
        "commentCount": 5,
        "createdAt": "2026-10-26T09:30:00Z",
        "tags": [
          "story",
          "ask_hn"
        ],
        "storyText": "We fuzz the parser.\n\nWhat else catches miscompilations early?"
      }
    ],
    "page": 1,
    "pages": 3,
    "totalHits": 5,
    "hitsPerPage": 2,
    "nextPage": 2
  },
  {
    "stories": [
      {
        "id": 41980,
        "title": "Notes on compiler passes",
        "url": "https://example.com/passes",
        "discussionUrl": "https://news.ycombinator.com/item?id=41980",
        "author": "notetaker",
        "points": 1,
        "commentCount": 0,
        "createdAt": "2026-10-26T08:15:00Z",
        "tags": [
          "story"
        ]
      }
    ],
    "page": 2,
    "pages": 3,
    "totalHits": 5,
    "hitsPerPage": 2,
    "nextPage": 0
  }
];

/** Select one zero-based page from the manifest's compilerStories scenario. */
export function searchStoriesCompilerStories(page = 0): Case<StorySearchPage> {
  const selected = compilerStoriesPages[page];
  if (!selected) throw new RangeError('page outside the manifest mock');
  return branchCase('searched', structuredClone(selected));
}

/** Return the manifest's default story search page. */
export function searchStoriesDefault(page = 0): Case<StorySearchPage> { return searchStoriesCompilerStories(page); }

class SearchMock<Input extends { page?: number | undefined }, Output> extends MockQuery<Input, Output> {
  private useManifestPages = true;
  private invalidPageRequested = false;

  constructor(definition: QueryDefinition, private readonly pages: readonly Output[], private readonly empty: Output) {
    super(definition);
  }

  override default(...cases: Case<Output>[]): this {
    this.useManifestPages = false;
    return super.default(...cases);
  }

  override invoke(call: Call, input: Input): QueryAttempt<Output> {
    if (!this.useManifestPages) return super.invoke(call, input);
    const selected = this.pages[input.page ?? 0];
    // MockQuery resolves synchronously. Scoped scripts, queued answers and strict mode take precedence over this fallback.
    if (selected !== undefined) {
      super.default(branchCase('searched', structuredClone(selected)));
      return super.invoke(call, input);
    }
    const missing = structuredClone(this.empty);
    const failure: Failure = { kind: 'LOCAL_DEFECT', provider: call.operation.connectorId, operation: call.operation.operationId, message: 'requested page is outside the scripted listing' };
    super.default(branchCase('defect', missing, failure));
    const attempt = super.invoke(call, input);
    if (attempt.kind === 'branch' && attempt.value === missing) this.invalidPageRequested = true;
    return attempt;
  }

  override assertNoFailures(): void {
    super.assertNoFailures();
    if (this.invalidPageRequested) throw new Error('requested page is outside the scripted listing');
  }
}

/** Mock public Hacker News operations, including the manifest's zero-based search pages. */
export class GeneratedMock extends ConnectorMock {
  private readonly itemSearch = new SearchMock<SearchItemsInput, ItemSearchPage>(SearchItemsDefinition, publicItemsPages, emptyItemSearchPage);
  private readonly storySearch = new SearchMock<SearchStoriesInput, StorySearchPage>(SearchStoriesDefinition, compilerStoriesPages, emptyStorySearchPage);

  /** Script public item searches. The default selects the page requested by the Flow. */
  override searchItems(): MockQuery<SearchItemsInput, ItemSearchPage> { return this.itemSearch; }
  /** Script story searches. The default selects the page requested by the Flow. */
  override searchStories(): MockQuery<SearchStoriesInput, StorySearchPage> { return this.storySearch; }
  /** Report unscripted calls and out-of-range manifest pages across all operations. */
  override assertNoFailures(): void {
    super.assertNoFailures();
    this.itemSearch.assertNoFailures();
    this.storySearch.assertNoFailures();
  }
}
