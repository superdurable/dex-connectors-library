// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetSubscriptionStep, type Connection, type SubscriptionStatus, type GetSubscriptionInput,
} from '@superdurable/connector-chargebee';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'chargebee';
const subscriptionAttribute = new Attribute<SubscriptionStatus>('chargebee-subscription', jsonCodec<SubscriptionStatus>());

/** Read one subscription through a typed Chargebee Query. */
export class SubscriptionInspectionFlow implements Flow<GetSubscriptionInput> {
  private readonly complete = new CompleteSubscriptionInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Chargebee connection name differs');
    this.read = newGetSubscriptionStep<GetSubscriptionInput>({
      stepType: 'CallGetSubscription',
      annotations: { groupId: 'provider', groupLabel: 'Chargebee', explanation: 'Read one subscription from Chargebee.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteChargebee'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'ChargebeeSubscriptionInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetSubscriptionInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected subscription result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [subscriptionAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected subscription and complete the Flow."
class CompleteSubscriptionInspection implements Step<QueryResult<SubscriptionStatus>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteChargebee'; }
  /** Persist and return the selected subscription. */
  execute(context: Context, result: QueryResult<SubscriptionStatus>): StepDecision {
    subscriptionAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
