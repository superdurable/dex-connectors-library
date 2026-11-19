// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetSubscriberCountStep, type Connection, type GetSubscriberCountInput, type SubscriberCountResult } from '@superdurable/connector-wxpusher';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'wxpusher';
const resultAttribute = new Attribute<SubscriberCountResult>('wxpusher-result', jsonCodec<SubscriberCountResult>());

/** Query WxPusher through a typed connector Step. */
export class WxPusherInspectionFlow implements Flow<GetSubscriberCountInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('WxPusher connection name differs');
    this.read = newGetSubscriberCountStep<GetSubscriberCountInput>({
      stepType: 'CallGetSubscriberCount',
      annotations: { groupId: 'provider', groupLabel: 'WxPusher', explanation: 'Read provider data through WxPusher.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      counted: new StepRef('CompleteWxPusher'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'WxPusherInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetSubscriberCountInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<SubscriberCountResult>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteWxPusher'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<SubscriberCountResult>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
