// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ExcelClient } from '../src/client.js';
import { AppendTableRowsInputSchema, GetTableRowsInputSchema, GetValuesInputSchema, UpdateValuesInputSchema, type Credentials } from '../src/models.js';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/microsoft/excel/scenarios.json', import.meta.url));
const connection = { provider: 'microsoft', name: 'approvals' };
const credentials: Credentials = { authMethodId: 'microsoft-oauth', accessToken: new SecretString('excel-token-SENTINEL'),
  oauthClientId: '', oauthClientSecret: new SecretString(''), refreshToken: new SecretString('') };

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAttempt, '/v1.0'));

async function invokeAttempt(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const refreshDriver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false,
    refresh: async () => { recorder.mismatches.push('unexpected credential refresh'); throw new Error('unexpected credential refresh'); } };
  const transport = (request: ProviderRequest) => {
    const url = new URL(request.url);
    // Go records Request.URL.Path after percent decoding; the transport still receives the escaped URL.
    url.pathname = decodeURIComponent(url.pathname);
    return answerRecordedRequest(recorder, { ...request, url: url.toString() }, before => new ProviderTransportError(before));
  };
  const client = new ExcelClient({}, new StaticCredentialProvider([[connection, credentials]]),
    { endpoint: 'http://127.0.0.1:12345/v1.0', transport, refreshDriver, now: () => context.fixedClock });
  const call = scenarioCall(scenario, connection, 'microsoft-excel', context) as Call;
  if (scenario.operation === 'getValues') return client.getValues().invoke(call, GetValuesInputSchema.parse(scenario.input));
  if (scenario.operation === 'getTableRows') return client.getTableRows().invoke(call, GetTableRowsInputSchema.parse(scenario.input));
  if (scenario.operation === 'updateValues') return productionMutation(client.updateValues(), call, UpdateValuesInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'appendTableRows') return productionMutation(client.appendTableRows(), call, AppendTableRowsInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new TypeError(`unknown Excel operation ${scenario.operation}`);
}
