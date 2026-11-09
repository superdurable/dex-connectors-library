// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListMediaStep, type Connection, type ListMediaInput, type MediaListPage } from '@superdurable/connector-meta-instagram-business';

/** Static Dex Web connection for the Instagram Business example. */
export const ConnectionName = 'instagram-business';
const resultAttribute = new Attribute<MediaListPage>('instagram-business-media-page', jsonCodec<MediaListPage>());

/** List media visible to the connected Instagram User token. */
export class MediaListFlow implements Flow<ListMediaInput> {
  private readonly complete = new CompleteMediaList();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Instagram Business connection name differs');
    this.read = newListMediaStep<ListMediaInput>({
      stepType: 'CallListMedia',
      annotations: { groupId: 'provider', groupLabel: 'Instagram Business', explanation: 'List Instagram media IDs and captions using the User token.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'InstagramBusinessMediaListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListMediaInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the media page and complete the Flow."
class CompleteMediaList implements Step<QueryResult<MediaListPage>> {
  readonly inputCodec = jsonCodec<QueryResult<MediaListPage>>();
  getStepType(): string { return 'CompleteListMedia'; }
  execute(context: Context, result: QueryResult<MediaListPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
