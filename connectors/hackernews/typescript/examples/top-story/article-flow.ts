// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetArticleThreadStep, type Connection, type ArticleThread } from '@superdurable/connector-hackernews';

const ConnectionName = 'hackernews-public';

/** Select the positive Hacker News article ID to read. */
export interface ArticleThreadRequest { articleId: number }

/** Read an article and its indexed public comments. */
export class ArticleThreadFlow implements Flow<ArticleThreadRequest> {
  private readonly complete = new ReturnArticleThread();
  private readonly read;

  /** Bind the Worker's static public connection. */
  constructor(connection: Connection) {
    this.read = newGetArticleThreadStep<ArticleThreadRequest>({
      stepType: 'ReadHackerNewsArticleThread',
      annotations: { groupId: 'article', groupLabel: 'Article thread', explanation: 'Read the selected article and its indexed public comments.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => ({ id: input.articleId }),
      found: new StepRef('ReturnHackerNewsArticleThread'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'HackerNewsArticleThreadTypeScript'; }
  /** Register the provider read and completion Steps. */ getSteps(): StepList<ArticleThreadRequest> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
}

// dex:group group-id:article group-label:"Article thread"
// dex:explanation text:"Return the article and its nested comments."
class ReturnArticleThread implements Step<QueryResult<ArticleThread>> {
  readonly inputCodec = jsonCodec<QueryResult<ArticleThread>>();
  getStepType(): string { return 'ReturnHackerNewsArticleThread'; }
  execute(_context: Context, input: QueryResult<ArticleThread>): StepDecision {
    return gracefulComplete(input.value);
  }
}
