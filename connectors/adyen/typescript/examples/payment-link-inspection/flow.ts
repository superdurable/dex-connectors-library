// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetPaymentLinkStep, type Connection, type PaymentLinkStatus, type GetPaymentLinkInput,
} from '@superdurable/connector-adyen';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'adyen';
const paymentLinkAttribute = new Attribute<PaymentLinkStatus>('adyen-payment-link', jsonCodec<PaymentLinkStatus>());

/** Read one payment link through a typed Adyen Query. */
export class PaymentLinkInspectionFlow implements Flow<GetPaymentLinkInput> {
  private readonly complete = new CompletePaymentLinkInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Adyen connection name differs');
    this.read = newGetPaymentLinkStep<GetPaymentLinkInput>({
      stepType: 'CallGetPaymentLink',
      annotations: { groupId: 'provider', groupLabel: 'Adyen', explanation: 'Read one payment link from Adyen.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteAdyen'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'AdyenPaymentLinkInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetPaymentLinkInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected payment link result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [paymentLinkAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected payment link and complete the Flow."
class CompletePaymentLinkInspection implements Step<QueryResult<PaymentLinkStatus>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteAdyen'; }
  /** Persist and return the selected payment link. */
  execute(context: Context, result: QueryResult<PaymentLinkStatus>): StepDecision {
    paymentLinkAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
