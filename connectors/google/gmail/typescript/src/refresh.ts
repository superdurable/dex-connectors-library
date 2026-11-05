// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, type CredentialRefreshDriver, type CredentialRefreshResult, type CredentialRefreshState } from '@superdurable/connectors-sdk';
import { isRefreshRequired, ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Credentials } from './models.js';
import { GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID } from './models.js';

/** Refresh Gmail credentials using generated grant declarations and Google-specific validation. */
export class GmailCredentialRefreshDriver implements CredentialRefreshDriver<Credentials> {
  readonly #manifestDriver: ManifestCredentialRefreshDriver<Credentials>;

  /** Inject the token transport and clock without transferring ownership. */
  constructor(fetchImplementation: typeof fetch = fetch, now: () => Date = () => new Date()) {
    if (!fetchImplementation || !now) throw new TypeError('Gmail token transport and clock are required');
    this.#manifestDriver = new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Google', auth: Authorization, credentialFields: CredentialFields, fetch: fetchImplementation, now,
    });
  }

  /** Refresh absent credentials and tokens approaching expiry, including missing expiry. */
  refreshRequired(state: CredentialRefreshState<Credentials>): boolean {
    return isRefreshRequired(Boolean(state.credentials.accessToken.reveal()), state.expiresAt, state.now, 'refresh');
  }

  /** Apply Gmail's method and delegated mailbox checks before the declared token exchange. */
  async refresh(state: CredentialRefreshState<Credentials>, signal: AbortSignal): Promise<CredentialRefreshResult<Credentials>> {
    const credentials = state.credentials;
    const method = credentials.authMethodId || GoogleOAuthAuthMethodID;
    if (method !== GoogleOAuthAuthMethodID && method !== WorkspaceDomainDelegationAuthMethodID)
      throw new TypeError('Gmail authorization method is not supported');
    if (method !== WorkspaceDomainDelegationAuthMethodID) return this.#manifestDriver.refresh(state, signal);

    this.#validateServiceAccountKey(credentials.serviceAccountKey?.reveal() ?? '');
    const delegatedUser = (credentials.delegatedUser ?? '').trim();
    if (!delegatedUser || !delegatedUser.includes('@')) throw new ReauthorizationRequiredError();
    const result = await this.#manifestDriver.refresh({ ...state, credentials: { ...credentials, delegatedUser } }, signal);
    return { ...result, credentials: { ...result.credentials, delegatedUser: credentials.delegatedUser } };
  }

  #validateServiceAccountKey(raw: string): void {
    let key: Record<string, unknown>;
    try {
      const parsed: unknown = JSON.parse(raw);
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new TypeError();
      key = parsed as Record<string, unknown>;
    } catch { throw new ReauthorizationRequiredError(); }
    if (typeof key.client_email !== 'string' || !key.client_email ||
      typeof key.private_key !== 'string' || !key.private_key ||
      typeof key.token_uri !== 'string' || !key.token_uri) throw new ReauthorizationRequiredError();
    try {
      const endpoint = new URL(key.token_uri);
      if (endpoint.protocol !== 'https:' || !endpoint.hostname) throw new TypeError();
    } catch { throw new ReauthorizationRequiredError(); }
  }
}
