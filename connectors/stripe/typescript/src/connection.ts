// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ConnectionRefSchema, SecretString, type Call, type ConnectionRef, type CredentialProvider, type Mutation, type Query } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import type { CheckoutSession, CreateACHCheckoutSessionInput, CreateCardCheckoutSessionInput, CreateRefundInput, GetCheckoutSessionInput, GetPaymentIntentInput, PaymentIntent, Refund } from './models.js';
import type { Config, Credentials } from './models.js';
import { StripeClient, type StripeClientOptions } from './client.js';

/** One typed operation accessor per Stripe manifest operation. */
export interface Operations {
  /** Hosted card Checkout mutation. */ createCardCheckoutSession(): Mutation<CreateCardCheckoutSessionInput, CheckoutSession>;
  /** Hosted ACH Checkout mutation. */ createACHCheckoutSession(): Mutation<CreateACHCheckoutSessionInput, CheckoutSession>;
  /** Checkout Session query. */ getCheckoutSession(): Query<GetCheckoutSessionInput, CheckoutSession>;
  /** PaymentIntent query. */ getPaymentIntent(): Query<GetPaymentIntentInput, PaymentIntent>;
  /** Refund mutation. */ createRefund(): Mutation<CreateRefundInput, Refund>;
}

/** Named Stripe operation connection; this object must remain outside Flow state. */
export class Connection {
  /** Static provider and connection name used by each Step factory. */
  readonly reference: ConnectionRef;
  /** Bound operation implementations. */
  readonly operations: Operations;

  /** Validate one static connection name and own its operations. */
  constructor(reference: ConnectionRef, operations: Operations) {
    ConnectionRefSchema.parse(reference);
    if (reference.provider !== 'stripe') throw new TypeError('Stripe connection provider must be stripe');
    if (!operations) throw new TypeError('Stripe connector operations are required');
    this.reference = reference;
    this.operations = operations;
  }

  /** Prevent accidental serialization of connection and credential adapters. */
  toJSON(): never { throw new TypeError('Stripe connector connections cannot be serialized'); }
}

/** Bind a Stripe API client to one static connection reference. */
export function newConnection(client: StripeClient, reference: ConnectionRef): Connection {
  if (!client) throw new TypeError('Stripe connector client is required');
  return new Connection(reference, client);
}

/** Bind test operations or another implementation to one static connection reference. */
export function newConnectionWithOperations(operations: Operations, reference: ConnectionRef): Connection {
  return new Connection(reference, operations);
}

/** Open a manifest-declared Stripe connection from a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: StripeClientOptions = {}): Connection {
  if (!project) throw new TypeError('Stripe connection requires the loaded project configuration');
  const selected = project.configuration.connections.find(connection => connection.connectorId === 'stripe' && connection.connectionName === connectionName);
  if (!selected) throw new TypeError('Stripe project connection is not configured');
  if (selected.provider !== 'stripe') throw new TypeError('Stripe project connection provider is invalid');
  const settings = z.object({ endpoint: z.string().optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration);
  const config: Config = {
    ...(settings.endpoint !== undefined ? { endpoint: settings.endpoint } : {}),
    ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
  };
  const credentials: CredentialProvider<Credentials> = {
    async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
      void call;
      const fields = await project.resolveCredential('stripe', connectionName, signal);
      const decoded = z.object({
        auth_method: z.enum(['api-key-only', 'api-key-with-webhook']).optional(),
        secret_key: z.string(), webhook_secret: z.string().optional(),
      }).parse(fields);
      const authMethodId = decoded.auth_method ?? selected.authMethodId ?? 'api-key-with-webhook';
      if (authMethodId !== 'api-key-only' && authMethodId !== 'api-key-with-webhook') throw new TypeError('Stripe credential auth_method is invalid');
      return {
        authMethodId, secretKey: new SecretString(decoded.secret_key),
        ...(decoded.webhook_secret ? { webhookSecret: new SecretString(decoded.webhook_secret) } : {}),
      };
    },
  };
  return newConnection(new StripeClient(config, credentials, options), { provider: 'stripe', name: connectionName });
}
