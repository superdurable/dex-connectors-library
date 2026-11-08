// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetPaymentStep, type Connection, type PaymentStatus, type GetPaymentInput,
} from '@superdurable/connector-razorpay';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'razorpay';
const paymentAttribute = new Attribute<PaymentStatus>('razorpay-payment', jsonCodec<PaymentStatus>());

/** Read one payment through a typed Razorpay Query. */
export class PaymentInspectionFlow implements Flow<GetPaymentInput> {
  private readonly complete = new CompletePaymentInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Razorpay connection name differs');
    this.read = newGetPaymentStep<GetPaymentInput>({
      stepType: 'CallGetPayment',
      annotations: { groupId: 'provider', groupLabel: 'Razorpay', explanation: 'Read one payment from Razorpay.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteRazorpay'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'RazorpayPaymentInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetPaymentInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected payment result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [paymentAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected payment and complete the Flow."
class CompletePaymentInspection implements Step<QueryResult<PaymentStatus>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteRazorpay'; }
  /** Persist and return the selected payment. */
  execute(context: Context, result: QueryResult<PaymentStatus>): StepDecision {
    paymentAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
