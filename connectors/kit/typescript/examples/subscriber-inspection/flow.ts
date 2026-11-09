// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListSubscribersStep, type Connection, type ListSubscribersInput, type SubscribersPage } from '@superdurable/connector-kit';

/** Static Dex Web connection for the Kit example. */
export const ConnectionName = 'kit';
const resultAttribute = new Attribute<SubscribersPage>('kit-subscribers-page', jsonCodec<SubscribersPage>());

/** List subscribers visible to the connected Kit account. */
export class SubscriberInspectionFlow implements Flow<ListSubscribersInput> {
  private readonly complete = new CompleteSubscriberInspection();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Kit connection name differs');
    this.read = newListSubscribersStep<ListSubscribersInput>({
      stepType: 'CallListSubscribers',
      annotations: { groupId: 'provider', groupLabel: 'Kit', explanation: 'List subscribers accessible to the connected account.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'KitSubscriberInspection'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListSubscribersInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the subscriber page and complete the Flow."
class CompleteSubscriberInspection implements Step<QueryResult<SubscribersPage>> {
  readonly inputCodec = jsonCodec<QueryResult<SubscribersPage>>();
  getStepType(): string { return 'CompleteListSubscribers'; }
  execute(context: Context, result: QueryResult<SubscribersPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
