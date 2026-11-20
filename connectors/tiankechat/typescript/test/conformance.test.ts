// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TiankeChatClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, SyntheticRandomSource, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/tiankechat/scenarios.json', import.meta.url));
const connection = { provider: 'tiankechat', name: 'tiankechat' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTiankeChat, ''));
async function invokeTiankeChat(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listKnowledgeEntries') throw new Error(`unknown TiankeChat operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appId: 'ap_REDACTED_APP_ID', appSecret: new SecretString('tks_REDACTED_APP_SECRET') }]]);
  const client = new TiankeChatClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-19T12:00:00Z'), nonce: () => new SyntheticRandomSource(scenario.randomSeed ?? 'SYNTHETIC_CONFORMANCE_TIANKECHAT').read(16) });
  const call: Call = { context: { flowId: 'tiankechat-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'tiankechat', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.listKnowledgeEntries().invoke(call, scenario.input as unknown as Parameters<ReturnType<TiankeChatClient['listKnowledgeEntries']>['invoke']>[1]);
}
