// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetPostStep, type Connection, type PostMetadata, type GetPostInput,
} from '@superdurable/connector-x-twitter';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'x-twitter';
const postAttribute = new Attribute<PostMetadata>('x-post', jsonCodec<PostMetadata>());

/** Read one post through a typed X Query. */
export class PostInspectionFlow implements Flow<GetPostInput> {
  private readonly complete = new CompletePostInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('X connection name differs');
    this.read = newGetPostStep<GetPostInput>({
      stepType: 'CallGetPost',
      annotations: { groupId: 'provider', groupLabel: 'X', explanation: 'Read one post from X.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteX'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'XPostInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetPostInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected post result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [postAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected post and complete the Flow."
class CompletePostInspection implements Step<QueryResult<PostMetadata>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteX'; }
  /** Persist and return the selected post. */
  execute(context: Context, result: QueryResult<PostMetadata>): StepDecision {
    postAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
