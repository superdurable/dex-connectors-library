// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetPaymentStep, type Connection, type GetPaymentInput, type Payment } from '@superdurable/connector-square';

/** Static Dex Web connection name. */
export const ConnectionName = 'square';
/** Safe payment ID retained in Flow state. */
export const paymentRequest = new Attribute<GetPaymentInput>('square-payment-request', jsonCodec<GetPaymentInput>());
/** Safe payment summary retained in Flow state. */
export const paymentResult = new Attribute<Payment>('square-payment-result', jsonCodec<Payment>());

/** Read one Square payment through a Dex Query Step. */
export class ResourceReadFlow implements Flow<GetPaymentInput> {
  private readonly prepare = new PreparePaymentRead();
  private readonly complete = new CompletePaymentRead();
  /** Bind the named connection. */ constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Square connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'SquareResourceReadFlow'; }
  /** Register preparation, provider read, and completion. */
  getSteps(): StepList<GetPaymentInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetPaymentStep({ stepType: 'ReadSquarePayment', annotations: { groupId: 'square-read', groupLabel: 'Square Payment', explanation: 'Read one payment from Square.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, found: this.complete }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [paymentRequest, paymentResult] }; }
}

/** Validate and retain the requested payment ID. */
// dex:group group-id:square-read group-label:"Square Payment"
// dex:explanation text:"Validate the payment ID before the provider read."
export class PreparePaymentRead implements Step<GetPaymentInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareSquarePaymentRead'; }
  /** Start the provider Step. */ execute(context: Context, input: GetPaymentInput): StepDecision {
    if (!input.paymentId) return forceFail('paymentId is required');
    paymentRequest.set(context, input);
    return goTo(new StepRef('ReadSquarePayment'), input);
  }
}

/** Save the safe payment summary and finish. */
// dex:group group-id:square-read group-label:"Square Payment"
// dex:explanation text:"Persist the returned payment summary and complete."
export class CompletePaymentRead implements Step<QueryResult<Payment>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteSquarePaymentRead'; }
  /** Complete the Flow with provider fields. */ execute(context: Context, result: QueryResult<Payment>): StepDecision {
    paymentResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
