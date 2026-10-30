// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';

/** A hosted Stripe Checkout Session safe for Dex persistence. */
export const CheckoutSessionSchema = z.strictObject({
  id: z.string(), url: z.string().optional(), clientReferenceId: z.string().optional(),
  paymentStatus: z.string().optional(), status: z.string().optional(),
  paymentIntentId: z.string().optional(), currency: z.string().optional(),
  amountTotal: z.number().int().optional(), expiresAt: z.iso.datetime({ offset: true }).optional(),
  metadata: z.record(z.string(), z.string()).optional(),
});
/** A hosted Stripe Checkout Session safe for Dex persistence. */
export type CheckoutSession = z.infer<typeof CheckoutSessionSchema>;

/** One hosted ACH Checkout request; money uses minor currency units. */
export interface CreateACHCheckoutSessionInput {
  /** Nonsecret application reference, at most 200 characters. */ clientReferenceId: string;
  /** One plain customer email address. */ customerEmail: string;
  /** Absolute HTTPS redirect after success. */ successUrl: string;
  /** Absolute HTTPS redirect after cancellation. */ cancelUrl: string;
  /** Lowercase three-letter currency. */ currency: string;
  /** Positive price in minor currency units. */ unitAmount: number;
  /** Product label shown by Checkout. */ productName: string;
  /** Bounded, nonsecret application metadata. */ metadata?: Record<string, string>;
  /** Optional provider expiry, encoded as a Unix second. */ expiresAt?: string;
}

/** Runtime validation for one hosted Checkout input before provider dispatch. */
export const CreateCheckoutSessionInputSchema = z.object({
  clientReferenceId: z.string(), customerEmail: z.string(), successUrl: z.string(), cancelUrl: z.string(),
  currency: z.string(), unitAmount: z.number().int(), productName: z.string(),
  metadata: z.record(z.string(), z.string()).optional(), expiresAt: z.iso.datetime({ offset: true }).optional(),
});

/** One hosted card Checkout request with the same validation as ACH. */
export type CreateCardCheckoutSessionInput = CreateACHCheckoutSessionInput;

/** Stripe Checkout Session lookup input. */
export interface GetCheckoutSessionInput {
  /** Stripe cs_ identifier. */ sessionId: string;
}

/** Runtime validation for a Checkout Session lookup. */
export const GetCheckoutSessionInputSchema = z.object({ sessionId: z.string() });

/** Stripe PaymentIntent lookup input. */
export interface GetPaymentIntentInput {
  /** Stripe pi_ identifier. */ paymentIntentId: string;
}

/** Runtime validation for a PaymentIntent lookup. */
export const GetPaymentIntentInputSchema = z.object({ paymentIntentId: z.string() });

/** Safe PaymentIntent summary, with its latest charge refund total. */
export const PaymentIntentSchema = z.strictObject({
  id: z.string(), amount: z.number().int(), currency: z.string(), status: z.string(),
  amountRefunded: z.number().int(), customerEmail: z.string().optional(), latestChargeId: z.string().optional(),
});
/** Safe PaymentIntent summary, with its latest charge refund total. */
export type PaymentIntent = z.infer<typeof PaymentIntentSchema>;

/** One keyed Stripe refund request; money uses minor currency units. */
export interface CreateRefundInput {
  /** Stripe pi_ identifier. */ paymentIntentId: string;
  /** Positive refund amount in minor currency units. */ amount: number;
  /** Optional Stripe reason. */ reason?: 'duplicate' | 'fraudulent' | 'requested_by_customer';
  /** Bounded, nonsecret application metadata. */ metadata?: Record<string, string>;
}

/** Runtime validation for one refund input before provider dispatch. */
export const CreateRefundInputSchema = z.object({
  paymentIntentId: z.string(), amount: z.number().int(),
  reason: z.enum(['duplicate', 'fraudulent', 'requested_by_customer']).optional(),
  metadata: z.record(z.string(), z.string()).optional(),
});

/** Safe Stripe refund result. */
export const RefundSchema = z.strictObject({
  refundId: z.string(), status: z.string(), amount: z.number().int(), currency: z.string(),
});
/** Safe Stripe refund result. */
export type Refund = z.infer<typeof RefundSchema>;

/** Connection settings; blank endpoint and zero size use manifest defaults. */
export interface Config {
  /** Stripe v1 API base URL, or an explicitly supported compatible gateway. */ endpoint?: string;
  /** Maximum complete API response size in bytes. */ maxResponseBytes?: number;
}

/** Private Stripe credential resolved for each provider call. */
export interface Credentials {
  /** Manifest authorization method identity. */ authMethodId: 'api-key-only' | 'api-key-with-webhook';
  /** Stripe restricted or secret API key, held outside Flow state. */ secretKey: { reveal(): string };
  /** Signing secret for the webhook authorization method. */ webhookSecret?: { reveal(): string };
}
