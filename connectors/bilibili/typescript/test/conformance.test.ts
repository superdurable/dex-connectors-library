// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { BilibiliClient } from '../src/client.js';
import type { Credentials, GetAuthorizedUserInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/bilibili/scenarios.json', import.meta.url));
const connection = { provider: 'bilibili', name: 'authorized-user' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBilibili));

async function invokeBilibili(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials: Credentials = scenario.name === 'getAuthorizedUser-defect'
    ? { clientID: '', appSecret: new SecretString(''), accessToken: new SecretString('') }
    : { clientID: 'client123', appSecret: new SecretString('secret456'), accessToken: new SecretString('token789') };
  const source = new StaticCredentialProvider([[connection, credentials]]);
  const client = new BilibiliClient({ baseUrl: 'http://127.0.0.1' }, source, {
    send, now: () => new Date(1_730_185_575_000), nonce: () => new Uint8Array(16),
  });
  const call = scenarioCall(scenario, connection, 'bilibili', {
    flowId: 'bilibili-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation !== 'getAuthorizedUser') throw new Error(`unknown Bilibili operation ${scenario.operation}`);
  return client.getAuthorizedUser().invoke(call, scenario.input as GetAuthorizedUserInput);
}
