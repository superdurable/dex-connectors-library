// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { QuickBooksClient } from '../src/client.js';
import { CreateCustomerInputSchema, CreateInvoiceInputSchema, FindCustomerInputSchema, GetInvoiceInputSchema, ListInvoicesInputSchema, RecordPaymentInputSchema, SendInvoiceInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/intuit/quickbooks/scenarios.json', import.meta.url));
const connection = { provider: 'quickbooks', name: 'quickbooks-test' };
const clientID = 'ABtestQuickBooksClientID0123456789';
const token = Buffer.from(JSON.stringify({ sub: '8b9f5a10', aud: [clientID], iss: 'https://oauth.platform.intuit.com/op/v1', exp: 1759312800, realmid: '9341453050298464' })).toString('base64url');
const idToken = `e30.${token}.signature`;

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeQuickBooks, '/v3/company/9341453050298464'));

async function invokeQuickBooks(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = { accessToken: new SecretString('intuit-test-access-token-0123456789'), clientId: clientID, clientSecret: new SecretString('client-secret'), refreshToken: new SecretString(''), idToken: new SecretString(idToken) };
  const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };
  const client = new QuickBooksClient({}, source, { send, localProviderURL: 'http://127.0.0.1:12345' });
  const call = scenarioCall(scenario, connection, 'quickbooks', { flowId: 'quickbooks-flow', cancellationSignal: new AbortController().signal }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'findCustomer') return client.findCustomer().invoke(call, FindCustomerInputSchema.parse(scenario.input));
  if (scenario.operation === 'createCustomer') return productionMutation(client.createCustomer(), call, CreateCustomerInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'createInvoice') return productionMutation(client.createInvoice(), call, CreateInvoiceInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'getInvoice') return client.getInvoice().invoke(call, GetInvoiceInputSchema.parse(scenario.input));
  if (scenario.operation === 'listInvoices') return client.listInvoices().invoke(call, ListInvoicesInputSchema.parse(scenario.input));
  if (scenario.operation === 'sendInvoice') return productionMutation(client.sendInvoice(), call, SendInvoiceInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'recordPayment') return productionMutation(client.recordPayment(), call, RecordPaymentInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown QuickBooks operation ${scenario.operation}`);
}
