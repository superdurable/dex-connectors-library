// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { parse as parseYAML } from 'yaml';
import { z } from 'zod';
import { SecretString, type Mutation, type Query } from '@superdurable/connectors-sdk';
import { StripeClient } from '../src/client.js';
import * as stripeMock from '../src/mock.js';
import type { CheckoutSession, PaymentIntent, Refund } from '../src/models.js';

const ManifestSchema = z.object({
  metadata: z.object({ name: z.string(), version: z.string() }),
  spec: z.object({
    provider: z.string(), languages: z.object({ typescript: z.object({ version: z.string() }) }),
    operations: z.array(z.object({
      name: z.string(), kind: z.enum(['query', 'mutation']), idempotency: z.enum(['none', 'required']),
      authorization: z.enum(['required', 'none']).optional(),
      branches: z.array(z.object({ id: z.string(), description: z.string(), optional: z.boolean().optional() })),
      execution: z.object({ executeMethodTimeout: z.string(), durability: z.enum(['async', 'sync']), retry: z.unknown().optional(), heartbeatTimeout: z.unknown().optional() }),
      mocks: z.array(z.object({ name: z.string(), branch: z.string().optional(), default: z.boolean().optional(),
        output: z.record(z.string(), z.unknown()).optional(),
        failure: z.object({ kind: z.string(), message: z.string() }).optional(),
        uncertain: z.object({ failure: z.object({ kind: z.string(), message: z.string() }) }).optional(),
      })),
    })),
  }),
});

const manifest = ManifestSchema.parse(parseYAML(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as unknown);
const stripe = new StripeClient({}, { resolve: async () => ({ authMethodId: 'api-key-only', secretKey: new SecretString('rk_test_fixture') }) });
const implementations = {
  createCardCheckoutSession: stripe.createCardCheckoutSession(),
  createACHCheckoutSession: stripe.createACHCheckoutSession(),
  getCheckoutSession: stripe.getCheckoutSession(),
  getPaymentIntent: stripe.getPaymentIntent(),
  createRefund: stripe.createRefund(),
};
const mockFactories = stripeMock as Record<string, unknown>;

function zeroValue(operationName: string): CheckoutSession | PaymentIntent | Refund {
  if (operationName === 'getPaymentIntent') return { id: '', amount: 0, currency: '', status: '', amountRefunded: 0 };
  if (operationName === 'createRefund') return { refundId: '', status: '', amount: 0, currency: '' };
  return { id: '' };
}

function factoryCase(factoryName: string): unknown {
  const factory = mockFactories[factoryName];
  expect(factory, `missing mock factory ${factoryName}`).toBeTypeOf('function');
  if (typeof factory !== 'function') throw new TypeError(`missing mock factory ${factoryName}`);
  return factory();
}

describe('Stripe manifest drift', () => {
  it('matches every operation identity, kind, authorization, route and Execute default', () => {
    expect(manifest.metadata.name).toBe('stripe');
    expect(manifest.spec.provider).toBe('stripe');
    expect(manifest.spec.languages.typescript.version).toBe('v0.1.0');
    const manifestNames = manifest.spec.operations.map(operation => operation.name).sort();
    expect(manifestNames).toEqual(Object.keys(implementations).sort());
    for (const operation of manifest.spec.operations) {
      const implementation = implementations[operation.name as keyof typeof implementations] as Query<unknown, unknown> | Mutation<unknown, unknown>;
      expect(implementation.definition().operation).toEqual({ connectorId: 'stripe', operationId: operation.name });
      expect(implementation.definition().branches.map(branch => ({ id: branch.id, description: branch.description, optional: branch.optional ?? false })))
        .toEqual(operation.branches.map(branch => ({ id: branch.id, description: branch.description, optional: branch.optional ?? false })));
      expect(operation.authorization ?? 'required').toBe('required');
      expect(operation.execution).toMatchObject({ executeMethodTimeout: '30s', durability: 'async' });
      expect(operation.execution.retry).toBeUndefined();
      expect(operation.execution.heartbeatTimeout).toBeUndefined();
      expect(implementation.definition().stepDefaults).toEqual({ executeMethodTimeoutMs: 30_000, executeDurability: 'async' });
      expect('idempotencyKey' in implementation).toBe(operation.kind === 'mutation');
      expect(operation.idempotency).toBe(operation.kind === 'mutation' ? 'required' : 'none');
      if ('idempotencyKey' in implementation) expect(implementation.idempotencyKey('stable-call-id', {})).toBe('stable-call-id');
    }
  });

  it('matches every mock branch, output, failure and manifest default', () => {
    for (const operation of manifest.spec.operations) {
      const names = new Set<string>();
      const defaultScenario = operation.mocks.filter(scenario => scenario.default);
      expect(defaultScenario).toHaveLength(1);
      for (const scenario of operation.mocks) {
        expect(names.has(scenario.name)).toBe(false);
        names.add(scenario.name);
        const name = operation.name + scenario.name[0]?.toUpperCase() + scenario.name.slice(1);
        const actual = factoryCase(name);
        const expectedValue = scenario.output ?? zeroValue(operation.name);
        if (scenario.uncertain) {
          expect(actual).toMatchObject({ kind: 'uncertain', branch: 'uncertain', value: expectedValue,
            failure: { ...scenario.uncertain.failure, provider: 'stripe', operation: operation.name } });
        } else {
          expect(actual).toMatchObject({ kind: 'branch', branch: scenario.branch, value: expectedValue });
          if (scenario.failure) expect(actual).toMatchObject({ failure: { ...scenario.failure, provider: 'stripe', operation: operation.name } });
          else expect(actual).not.toHaveProperty('failure');
        }
      }
      const defaultName = operation.name + 'Default';
      const chosen = defaultScenario[0];
      if (!chosen) throw new TypeError('manifest default is missing');
      const chosenName = operation.name + chosen.name[0]?.toUpperCase() + chosen.name.slice(1);
      expect(factoryCase(defaultName)).toEqual(factoryCase(chosenName));
    }
  });
});
