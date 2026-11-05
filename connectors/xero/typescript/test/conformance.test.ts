// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { XeroClient } from '../src/client.js';
import { CreateInvoiceInputSchema, FindContactByEmailInputSchema, GetInvoiceInputSchema, ListContactsInputSchema, ListInvoicesInputSchema, RecordPaymentInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/xero/scenarios.json', import.meta.url));
const connection = { provider: 'xero', name: 'xero-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeXero));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeXero(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const isOAuth = scenario.name.startsWith('oauth-');
  const credentials = new StaticCredentialProvider([[connection, { authMethodId: isOAuth ? 'xero-oauth' : 'custom-connection', clientId: '0F1E2D3C4B5A69788796A5B4C3D2E1F0', clientSecret: new SecretString('client-secret'), accessToken: new SecretString('xero-test-access-token-0123456789'), ...(isOAuth ? { refreshToken: new SecretString('refresh-token') } : {}) }]]);
  const client = new XeroClient(isOAuth ? { organisation: scenario.name.includes('named-tenant') ? 'Maple Florist' : '70784a63-d24b-46a9-a4db-0e70a274b056' } : {}, credentials, { send: sender, localProviderURL: 'http://127.0.0.1:12345', now: () => new Date(scenario.fixedClock ?? '1970-01-01T00:00:01Z') });
  const call = scenarioCall(scenario, connection, 'xero', { flowId: 'xero-flow', cancellationSignal: new AbortController().signal }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listContacts') return client.listContacts().invoke(call, ListContactsInputSchema.parse(scenario.input));
  if (scenario.operation === 'findContactByEmail') return client.findContactByEmail().invoke(call, FindContactByEmailInputSchema.parse(scenario.input));
  if (scenario.operation === 'getInvoice') return client.getInvoice().invoke(call, GetInvoiceInputSchema.parse(scenario.input));
  if (scenario.operation === 'listInvoices') return client.listInvoices().invoke(call, ListInvoicesInputSchema.parse(scenario.input));
  if (scenario.operation === 'createInvoice') return productionMutation(client.createInvoice(), call, CreateInvoiceInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'recordPayment') return productionMutation(client.recordPayment(), call, RecordPaymentInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Xero operation ${scenario.operation}`);
}
