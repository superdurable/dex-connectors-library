// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { decodeGmailCredentials, decodeGmailSharedCredentials, encodeGmailCredentials } from '../src/credential_codec.js';

describe('Gmail private project credentials', () => {
  it('decodes legacy OAuth records and encodes only manifest fields for private storage', () => {
    const credentials = decodeGmailCredentials({ oauth_client_id: 'client', oauth_client_secret: 'secret', access_token: 'access', refresh_token: 'refresh', primary_email: 'owner@example.com' });
    expect(credentials.authMethodId).toBe('google-oauth');
    expect(credentials.accessToken.reveal()).toBe('access');
    expect(encodeGmailCredentials(credentials)).toEqual({ auth_method: 'google-oauth', oauth_client_id: 'client', oauth_client_secret: 'secret', access_token: 'access', refresh_token: 'refresh', primary_email: 'owner@example.com' });
    expect(() => JSON.stringify(credentials)).toThrow();
  });

  it('accepts a Workspace key before the delegated token exists and validates shared broker material', () => {
    const workspace = decodeGmailCredentials({ auth_method: 'workspace-domain-delegation', service_account_key: '{"private_key":"key"}', delegated_user: 'owner@example.com' });
    expect(workspace.accessToken.reveal()).toBe('');
    expect(encodeGmailCredentials(workspace)).toEqual({ auth_method: 'workspace-domain-delegation', service_account_key: '{"private_key":"key"}', delegated_user: 'owner@example.com' });
    expect(decodeGmailSharedCredentials({ auth_method: 'workspace-domain-delegation', delegated_user: 'owner@example.com', access_token: 'minted' }).accessToken.reveal()).toBe('minted');
    expect(() => decodeGmailSharedCredentials({ auth_method: 'workspace-domain-delegation', delegated_user: 'owner@example.com' })).toThrow();
  });

  it('rejects incomplete or unsupported private authorization without leaking values', () => {
    expect(() => decodeGmailCredentials({ auth_method: 'google-oauth', oauth_client_id: 'client', access_token: 'secret-token', primary_email: 'owner@example.com' })).toThrow('incomplete');
    expect(() => decodeGmailCredentials({ auth_method: 'other', access_token: 'secret-token' })).toThrow('invalid');
    expect(() => decodeGmailCredentials({ access_token: 42 })).toThrow('invalid');
  });
});
