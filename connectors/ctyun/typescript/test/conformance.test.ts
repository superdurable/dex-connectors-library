// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { CTYunClient, signAuthorization } from '../src/client.js';
import { ListKnowledgeBasesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/ctyun/scenarios.json', import.meta.url));
const connection = { provider: 'ctyun', name: 'ctyun' };
it('matches the Go fixed CTYun HMAC signature vector', () => {
  expect(signAuthorization('AKIDEXAMPLE', 'SKEXAMPLE', '123456789', '20211221T163614Z'))
    .toBe('AKIDEXAMPLE headers=ctyun-eop-request-id;eop-date Signature=SSs71iiml5WzAzRleNbbKLs2zyt4Oixio16KWIsAdgY=');
});
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCTYun, ''));

async function invokeCTYun(scenario: Scenario, recorder: ExchangeRecorder) {
  const isMissing = scenario.name === 'listKnowledgeBases-missing-credentials';
  const credentials = { accessKey: new SecretString(isMissing ? '' : 'AKIDEXAMPLE'), securityKey: new SecretString(isMissing ? '' : 'SKEXAMPLE'), tenantId: new SecretString(isMissing ? '' : 'tenant-example') };
  const client = new CTYunClient(scenario.name === 'listKnowledgeBases-oversize' ? { maxResponseBytes: 4 } : {},
    new StaticCredentialProvider([[connection, credentials]]), {
      now: () => new Date(scenario.fixedClock!), requestId: () => { if (scenario.name === 'listKnowledgeBases-id-generation-failure') throw new Error('synthetic failure'); return '123e4567-e89b-42d3-a456-426614174000'; },
      send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    });
  const call: Call = {
    context: { flowId: 'ctyun-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'ctyun', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listKnowledgeBases().invoke(call, ListKnowledgeBasesInputSchema.parse(scenario.input));
}
