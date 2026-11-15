// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, type TriggerEvent } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import { DocuSignClient } from '../src/client.js';
import { EnvelopeEventReceivedTriggerDefinition, newConnection, type EnvelopeEvent } from '../src/index.js';
import {
  ConnectionName, EnvelopeOutcomeFlow, EnvelopeOutcomeTriggerBinding, acceptEnvelopeEvent, mapToCreateEnvelopeInput, mapToEnvelopeEventInput, resolveFlowId,
} from '../examples/envelope-outcome/flow/workflow.js';

const envelopeId = '93be49ab-1111-2222-3333-f752070d71ec';
function event(customFields: EnvelopeEvent['customFields']): TriggerEvent<EnvelopeEvent> {
  return { id: `${envelopeId}:envelope-completed`, occurredAt: new Date('2026-10-04T13:00:00Z'), payload: {
    event: 'envelope-completed', envelopeId, accountId: 'a4ec37d6-1111-2222-3333-143885c220e1', generatedAt: '2026-10-04T13:00:00Z', ...(customFields ? { customFields } : {}),
  } };
}

describe('envelope outcome example', () => {
  it('correlates a Connect event to its Flow through the hidden custom field', () => {
    const correlated = event([{ name: 'other', value: 'x' }, { name: 'dexSigningRequestId', value: 'opp-123' }]);
    expect(acceptEnvelopeEvent(correlated)).toBe(true);
    expect(resolveFlowId(correlated)).toBe('docusign-signing-opp-123');
    expect(mapToEnvelopeEventInput(correlated)).toEqual({ eventId: `${envelopeId}:envelope-completed`, envelopeId, event: 'envelope-completed' });
    expect(acceptEnvelopeEvent(event(undefined)), 'an envelope this application did not send is consumed').toBe(false);
    expect(acceptEnvelopeEvent(event([{ name: 'dexSigningRequestId', value: '../other' }]))).toBe(false);
  });

  it('tags the envelope with the request ID', () => {
    expect(mapToCreateEnvelopeInput({ requestId: 'opp-123', templateId: '8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a', signers: [{ roleName: 'Customer', name: 'Priya Raman', email: 'priya@meridian.example.com' }] })).toEqual({
      templateId: '8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a', templateRoles: [{ roleName: 'Customer', name: 'Priya Raman', email: 'priya@meridian.example.com' }],
      customFields: [{ name: 'dexSigningRequestId', value: 'opp-123' }],
    });
  });

  it('declares the static binding without configuration units', () => {
    const connection = { provider: 'docusign', name: ConnectionName };
    const client = new DocuSignClient({}, new StaticCredentialProvider([[connection, { oauthClientId: 'key', oauthClientSecret: new SecretString('s'), accessToken: new SecretString('a'), refreshToken: new SecretString('r') }]]));
    const flow = new EnvelopeOutcomeFlow(newConnection(client, connection));
    expect(flow.getConnectorTriggerBindings()).toEqual([{ definition: EnvelopeEventReceivedTriggerDefinition, connectionName: ConnectionName, bindingName: EnvelopeOutcomeTriggerBinding }]);
    expect(flow.getFlowType()).toBe('DocuSignEnvelopeOutcomeFlow');
  });
});
