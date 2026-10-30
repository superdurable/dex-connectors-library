// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newCreateCardCheckoutSessionStep } from '../../../src/steps.js';
import type { StripeConnection } from '../../../src/connection.js';
import type { CheckoutSession, CreateCardCheckoutSessionInput } from '../../../src/models.js';

/** Static Dex Web connection name for this example. */
export const ConnectionName = 'stripe-payments';

/** Safe start input for one hosted card Checkout Session. */
export type CardCheckoutInput = CreateCardCheckoutSessionInput;

/** Durable, nonsecret request retained for inspection. */
export const cardCheckoutRequest = new Attribute<CardCheckoutInput>('card-checkout-request', jsonCodec<CardCheckoutInput>());

/** Durable, nonsecret Checkout Session summary retained for inspection. */
export const cardCheckoutSession = new Attribute<CheckoutSession>('card-checkout-session', jsonCodec<CheckoutSession>());

/** Card Checkout Flow with its provider Step supplied by the connector factory. */
export class CardCheckoutFlow implements Flow<CardCheckoutInput> {
  private readonly prepareStep = new PrepareCardCheckout();
  private readonly completeStep = new CompleteCardCheckout();
  private readonly checkoutStep: Step<CardCheckoutInput>;

  /** Bind the operation-specific Stripe Step at Worker registration. */
  constructor(connection: StripeConnection) {
    if (!connection) throw new TypeError('card Checkout Stripe connection is required');
    // dex:group group-id:card-checkout group-label:"Card Checkout"
    // dex:explanation text:"Create one hosted card Checkout Session."
    this.checkoutStep = newCreateCardCheckoutSessionStep({
      stepType: 'CreateCardCheckout',
      annotations: { groupId: 'card-checkout', groupLabel: 'Card Checkout', explanation: 'Create one hosted card Checkout Session.' },
      connection,
      connectionName: ConnectionName,
      mapToOperationInput: input => input,
      created: this.completeStep,
    });
  }

  /** Stable Dex Flow type. */
  getFlowType(): string { return 'CardCheckoutFlow'; }

  /** Register the start, provider, and completion Steps. */
  getSteps(): StepList<CardCheckoutInput> {
    return StepList.startStep(this.prepareStep).otherSteps(this.checkoutStep, this.completeStep);
  }

  /** Register every persisted request and result Attribute. */
  getPersistenceSchema(): PersistenceSchema {
    return { attributes: [cardCheckoutRequest, cardCheckoutSession] };
  }
}

/** Trim safe user fields before the provider Step receives them. */
export function prepareCardCheckoutInput(input: CardCheckoutInput): CardCheckoutInput | undefined {
  const prepared: CardCheckoutInput = {
    clientReferenceId: input.clientReferenceId?.trim(), customerEmail: input.customerEmail?.trim(),
    successUrl: input.successUrl?.trim(), cancelUrl: input.cancelUrl?.trim(),
    currency: input.currency?.trim().toLowerCase(), unitAmount: input.unitAmount,
    productName: input.productName?.trim(),
    ...(input.metadata ? { metadata: input.metadata } : {}),
    ...(input.expiresAt ? { expiresAt: input.expiresAt } : {}),
  };
  if (!prepared.clientReferenceId || !prepared.customerEmail || !prepared.successUrl ||
      !prepared.cancelUrl || !prepared.currency || !prepared.productName ||
      !Number.isSafeInteger(prepared.unitAmount) || prepared.unitAmount < 1) return undefined;
  return prepared;
}

/** Validate and retain the card Checkout request before the connector Step. */
// dex:group group-id:card-checkout group-label:"Card Checkout"
// dex:explanation text:"Validate and retain the card Checkout request."
export class PrepareCardCheckout implements Step<CardCheckoutInput> {
  /** Stable Dex Step type. */
  getStepType(): string { return 'PrepareCardCheckout'; }

  /** Store only safe request fields and move to card Checkout creation. */
  execute(context: Context, input: CardCheckoutInput): StepDecision {
    const prepared = prepareCardCheckoutInput(input);
    if (!prepared) return forceFail('all card Checkout fields are required and unitAmount must be positive');
    cardCheckoutRequest.set(context, prepared);
    return goTo(new StepRef('CreateCardCheckout'), prepared);
  }
}

/** Retain the successful provider result and complete the example. */
// dex:group group-id:card-checkout group-label:"Card Checkout"
// dex:explanation text:"Retain the safe card Checkout result."
export class CompleteCardCheckout implements Step<MutationResult<CheckoutSession>> {
  /** Stable Dex Step type. */
  getStepType(): string { return 'CompleteCardCheckout'; }

  /** Persist only the safe Session summary. */
  execute(context: Context, result: MutationResult<CheckoutSession>): StepDecision {
    cardCheckoutSession.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
