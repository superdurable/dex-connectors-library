// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { KlaviyoClient } from '../src/client.js';
import { CreateEventInputSchema, GetProfileInputSchema, type CreateEventInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/klaviyo/scenarios.json', import.meta.url));
const connection = { provider: 'klaviyo', name: 'klaviyo' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeKlaviyo));

async function invokeKlaviyo(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => {
    if (scenario.name === 'profile-punctuation-id') {
      expect(request.url).toBe('http://127.0.0.1:12345/api/profiles/profile%2Cone%3Btwo%21');
    }
    return answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  };
  const client = new KlaviyoClient({ baseUrl: 'http://127.0.0.1:12345/api' }, {
    resolve: async () => ({ privateApiKey: new SecretString('pk_test_key') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'klaviyo', {
    flowId: 'klaviyo-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'getProfile') return client.getProfile().invoke(call, GetProfileInputSchema.parse(scenario.input));
  if (scenario.operation === 'createEvent') return invokeProductionMutation(client.createEvent(), scenario.idempotencyKey!,
    recording => runMutation(recording as Mutation<CreateEventInput, { accepted: boolean }>, call, CreateEventInputSchema.parse(scenario.input)),
    error => error instanceof RetryError);
  throw new Error(`unknown Klaviyo operation ${scenario.operation}`);
}
