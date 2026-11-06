// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetItemStep, newListStoryIDsStep, type Connection, type Item, type StoryIDs } from '../../src/index.js';

/** Static Dex Web connection name used by the example. */
export const ConnectionName = 'hackernews-public';
/** Feed selection supplied when starting the example Flow. */
export interface TopStoryRequest { feed: string }
/** Direct source and discussion links returned by the Flow. */
export interface TopStoryOutcome { id: number; title: string; sourceUrl: string; discussionUrl: string }

/** Read a feed and its first item through two Hacker News Query Steps. */
export class TopStoryFlow implements Flow<TopStoryRequest> {
  private readonly completeItem = new CompleteItem();
  private readonly chooseStory = new ChooseStory();

  /** Bind the declared named public connection. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('top-story Flow requires its static connection');
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'HackerNewsTopStory'; }
  /** Register the start and both provider Steps. */
  getSteps(): StepList<TopStoryRequest> {
    return StepList.startStep<TopStoryRequest>(newListStoryIDsStep<TopStoryRequest>({
      stepType: 'ListTopStoryIDs',
      annotations: { groupId: 'hackernews', groupLabel: 'Hacker News', explanation: 'Read a bounded Hacker News feed snapshot.' },
      connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => ({ feed: input.feed, limit: 1 }), listed: this.chooseStory,
    })).otherSteps(this.chooseStory, newGetItemStep<{ id: number }>({
      stepType: 'GetTopStoryItem',
      annotations: { groupId: 'hackernews', groupLabel: 'Hacker News', explanation: 'Read the first item in the selected feed.' },
      connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => ({ id: input.id }), found: this.completeItem,
    }), this.completeItem);
  }
}

// dex:group group-id:hackernews group-label:"Hacker News"
// dex:explanation text:"Select the first provider-ordered item from the feed."
class ChooseStory implements Step<QueryResult<StoryIDs>> {
  getStepType(): string { return 'ChooseTopStory'; }
  execute(_context: Context, result: QueryResult<StoryIDs>): StepDecision {
    const id = result.value.ids?.[0];
    return id ? goTo(new StepRef('GetTopStoryItem'), { id }) : forceFail('Hacker News feed is empty');
  }
}

// dex:group group-id:hackernews group-label:"Hacker News"
// dex:explanation text:"Return the direct source and discussion links for the selected item."
class CompleteItem implements Step<QueryResult<Item>> {
  readonly inputCodec = jsonCodec<QueryResult<Item>>();
  getStepType(): string { return 'CompleteTopStory'; }
  execute(_context: Context, result: QueryResult<Item>): StepDecision {
    const item = result.value;
    return gracefulComplete({ id: item.id, title: item.title || '', sourceUrl: item.url || '', discussionUrl: item.discussionUrl } satisfies TopStoryOutcome);
  }
}
