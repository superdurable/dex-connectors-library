// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { LinkedInClient } from '../src/client.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/linkedin/scenarios.json', import.meta.url));
const connection = { provider: 'linkedin', name: 'signup' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeLinkedIn));

async function invokeLinkedIn(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider(scenario.name === 'missing-credential' ? [] : [[connection, { accessToken: new SecretString('one-use-token') }]]);
  const client = new LinkedInClient({ userInfoUrl: 'http://127.0.0.1/v2/userinfo' }, credentials, { send });
  const call = scenarioCall(scenario, connection, 'linkedin', {
    flowId: 'linkedin-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation !== 'getAuthenticatedProfile') throw new Error(`unknown LinkedIn operation ${scenario.operation}`);
  return client.getAuthenticatedProfile().invoke(call, scenario.input as Record<string, never>);
}
