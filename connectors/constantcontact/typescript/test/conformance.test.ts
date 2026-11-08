// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ConstantContactClient } from '../src/client.js';
import { GetContactInputSchema, type Credentials } from '../src/models.js';
import {
  answerRecordedRequest, loadScenarios, replay, scenarioCall,
  type ExchangeRecorder, type HeartbeatContext, type Scenario,
} from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/constantcontact/scenarios.json', import.meta.url));
const connection = { provider: 'constantcontact', name: 'constantcontact' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeContactAttempt, '/v3'));

async function invokeContactAttempt(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const credentials: Credentials = {
    oauthClientId: 'app-id', oauthClientSecret: new SecretString('app-secret'),
    accessToken: new SecretString('sample-access-token'), refreshToken: new SecretString('sample-refresh-token'),
  };
  const refreshDriver: CredentialRefreshDriver<Credentials> = {
    refreshRequired: () => false,
    refresh: async () => { recorder.mismatches.push('unexpected credential refresh'); throw new Error('unexpected refresh'); },
  };
  const client = new ConstantContactClient({ baseUrl: 'http://127.0.0.1:12345/v3' }, { resolve: async () => credentials }, {
    refreshDriver,
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call = scenarioCall(scenario, connection, 'constantcontact', context) as Call;
  if (scenario.operation === 'getContact') return client.getContact().invoke(call, GetContactInputSchema.parse(scenario.input));
  throw new Error(`unknown Constant Contact operation ${scenario.operation}`);
}
