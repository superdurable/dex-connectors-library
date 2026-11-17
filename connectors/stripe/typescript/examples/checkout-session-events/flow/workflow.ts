// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { defineCheckoutSessionUpdatedTriggerBinding, type CheckoutSessionEvent } from '@superdurable/connector-stripe';

/** Static Stripe connection selected by the Worker and Trigger. */ export const ConnectionName = 'stripe-payments';
/** Stable binding name for Checkout Session events. */ export const BindingName = 'registration-payments';
/** Persisted safe Checkout Session event. */ export const checkoutEventAttribute = new Attribute<CheckoutSessionEvent>('stripe-checkout-event', jsonCodec<CheckoutSessionEvent>());

/** Record one verified, settled Checkout Session event. */
export class CheckoutSessionEventFlow implements Flow<CheckoutSessionEvent> {
  private readonly record = new RecordCheckoutSessionEvent();
  /** Stable Flow identity. */ getFlowType(): string { return 'StripeCheckoutSessionEventFlow'; }
  /** Register the record Step. */ getSteps(): StepList<CheckoutSessionEvent> { return StepList.startStep(this.record); }
  /** Register the persisted event. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [checkoutEventAttribute] }; }
  /** Declare the binding shown in Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineCheckoutSessionUpdatedTriggerBinding({ connectionName: ConnectionName, bindingName: BindingName, configurationUI: { units: [] } })];
  }
}

/** Admit only confirmed delayed payments; other Checkout events remain available to other bindings. */
export function acceptSettledPayment(event: TriggerEvent<CheckoutSessionEvent>): boolean { return event.payload.type === 'checkout.session.async_payment_succeeded'; }
/** Derive a stable Flow ID from Stripe's event ID for redelivery deduplication. */
export function resolveFlowId(event: TriggerEvent<CheckoutSessionEvent>): string { return `stripe-${event.id}`; }
/** Store only the normalized session and event type. */
export function mapToFlowInput(event: TriggerEvent<CheckoutSessionEvent>): CheckoutSessionEvent { return event.payload; }

/** Persist one settled Checkout Session event. */
// dex:group group-id:stripe-checkout group-label:"Stripe Checkout"
// dex:explanation text:"Persist the verified settled Checkout Session and complete the Flow."
export class RecordCheckoutSessionEvent implements Step<CheckoutSessionEvent> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordStripeCheckoutSessionEvent'; }
  /** Store the safe session and complete. */
  execute(context: Context, event: CheckoutSessionEvent): StepDecision { checkoutEventAttribute.set(context, event); return gracefulComplete(event); }
}
