// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListPostsStep, type Connection, type ListPostsInput, type PostPage } from '@superdurable/connector-wordpress';

export const ConnectionName = 'wordpress';
const resultAttribute = new Attribute<PostPage>('wordpress-typescript-post-page', jsonCodec<PostPage>());

/** Read one page of published WordPress posts. */
export class PostListFlow implements Flow<ListPostsInput> {
  private readonly complete = new CompleteWordPressPostList();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('WordPress connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'WordPressTypeScriptPostListFlow'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListPostsInput> {
    return StepList.startStep(newListPostsStep<ListPostsInput>({
      stepType: 'CallWordPressListPosts',
      annotations: { groupId: 'post-list', groupLabel: 'Post list',
        explanation: 'Read one page of published WordPress posts.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [resultAttribute] };
  }
}

// dex:group group-id:post-list group-label:"Post list"
// dex:explanation text:"Persist the confirmed WordPress result and complete the Flow."
export class CompleteWordPressPostList implements Step<QueryResult<PostPage>> {
  readonly inputCodec = jsonCodec<QueryResult<PostPage>>();
  getStepType(): string { return 'CompleteWordPressPostList'; }
  execute(context: Context, result: QueryResult<PostPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
