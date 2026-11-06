// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListOutboundMessagesStep, type Connection, type ListOutboundMessagesInput, type MessagePage } from '../../src/index.js';

/** Static Dex Web connection used by this Worker and starter. */
export const ConnectionName = 'postmark';

/** Safe outbound page retained for Flow inspection. */
export const outboundPage = new Attribute<MessagePage>('postmark-outbound-lookup-page', jsonCodec<MessagePage>());

/** Query one page of outbound messages and retain its metadata. */
export class OutboundLookupFlow implements Flow<ListOutboundMessagesInput> {
  private readonly complete = new CompleteOutboundLookup();
  private readonly list;

  /** Bind the named Postmark connection at Worker registration. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('outbound lookup requires the Postmark connection');
    this.list = newListOutboundMessagesStep<ListOutboundMessagesInput>({
      stepType: 'ListPostmarkOutboundMessages',
      annotations: { groupId: 'postmark', groupLabel: 'Postmark', explanation: 'List one bounded page of outbound Postmark messages.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompletePostmarkOutboundLookup'),
    });
  }

  /** Stable Flow identity. */ getFlowType(): string { return 'PostmarkTypeScriptOutboundLookup'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListOutboundMessagesInput> { return StepList.startStep(this.list).otherSteps(this.complete); }
  /** Register the retained page Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [outboundPage] }; }
}

// dex:group group-id:postmark group-label:"Postmark"
// dex:explanation text:"Retain the returned outbound page and complete the Flow."
class CompleteOutboundLookup implements Step<QueryResult<MessagePage>> {
  getStepType(): string { return 'CompletePostmarkOutboundLookup'; }
  execute(context: Context, result: QueryResult<MessagePage>): StepDecision {
    outboundPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
