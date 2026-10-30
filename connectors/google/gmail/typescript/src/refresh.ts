// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { CredentialRefreshDriver, CredentialRefreshResult, CredentialRefreshState } from '@superdurable/connectors-sdk';
import { ReauthorizationRequiredError } from '@superdurable/connectors-sdk';
import {
  hasAllScopes, isRefreshRequired, parseRSAPrivateKeyPEM, signJWTBearerAssertion, TokenEndpoint,
} from '@superdurable/connectors-sdk/oauthtoken';
import { z } from 'zod';
import type { Credentials } from './models.js';
import { GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID } from './models.js';
import { parseMailbox } from './message.js';

const GoogleOAuthTokenEndpoint = 'https://oauth2.googleapis.com/token';
const RequiredGmailScopes = ['https://www.googleapis.com/auth/gmail.readonly', 'https://www.googleapis.com/auth/gmail.send'];
const TerminalErrorCodes = ['invalid_grant', 'invalid_client', 'unauthorized_client'];
const ServiceAccountKeySchema = z.object({ client_email: z.string().min(1), private_key: z.string().min(1), token_uri: z.url() });

/** Refresh Gmail OAuth or Workspace delegation credentials without owning persistence. */
export class GmailCredentialRefreshDriver implements CredentialRefreshDriver<Credentials> {
  readonly #fetch: typeof fetch;
  readonly #now: () => Date;

  /** Inject the token request transport and clock for deterministic verification. */
  constructor(fetchImplementation: typeof fetch = fetch, now: () => Date = () => new Date()) {
    if (!fetchImplementation || !now) throw new TypeError('Gmail token transport and clock are required');
    this.#fetch = fetchImplementation;
    this.#now = now;
  }

  /** Refresh absent credentials or tokens whose expiry is within five minutes. */
  refreshRequired(state: CredentialRefreshState<Credentials>): boolean {
    return isRefreshRequired(Boolean(state.credentials.accessToken.reveal()), state.expiresAt, state.now, 'refresh');
  }

  /** Make one Google token exchange after the host admits and serializes refresh. */
  async refresh(state: CredentialRefreshState<Credentials>, signal: AbortSignal): Promise<CredentialRefreshResult<Credentials>> {
    const credentials = state.credentials;
    if (!credentials.authMethodId || credentials.authMethodId === GoogleOAuthAuthMethodID) {
      if (!credentials.oauthClientId || !credentials.oauthClientSecret || !credentials.refreshToken) throw new ReauthorizationRequiredError();
      const token = await this.#tokenEndpoint(GoogleOAuthTokenEndpoint).exchangeRefreshToken({
        id: credentials.oauthClientId, secret: credentials.oauthClientSecret, authenticationMethod: 'client_secret_post',
      }, credentials.refreshToken, signal);
      this.#verifyScopes(token.scope);
      return {
        credentials: { ...credentials, accessToken: token.accessToken, refreshToken: token.nextRefreshToken(credentials.refreshToken) },
        expiresAt: new Date(this.#now().getTime() + token.expiresInMs),
      };
    }
    if (credentials.authMethodId !== WorkspaceDomainDelegationAuthMethodID) throw new ReauthorizationRequiredError();
    let key: z.infer<typeof ServiceAccountKeySchema>;
    try { key = ServiceAccountKeySchema.parse(JSON.parse(credentials.serviceAccountKey?.reveal() ?? '') as unknown); }
    catch { throw new ReauthorizationRequiredError(); }
    if (new URL(key.token_uri).protocol !== 'https:' || !parseMailbox(credentials.delegatedUser ?? '')) throw new ReauthorizationRequiredError();
    let assertion;
    try {
      assertion = signJWTBearerAssertion({
        issuer: key.client_email, subject: credentials.delegatedUser, audience: key.token_uri,
        scope: RequiredGmailScopes.join(' '), issuedAt: this.#now(), lifetimeMs: 3_600_000,
      }, parseRSAPrivateKeyPEM(key.private_key));
    } catch { throw new ReauthorizationRequiredError(); }
    const token = await this.#tokenEndpoint(key.token_uri).exchangeJWTBearerAssertion(assertion, signal);
    this.#verifyScopes(token.scope);
    return {
      credentials: { ...credentials, accessToken: token.accessToken, primaryEmail: credentials.delegatedUser ?? '' },
      expiresAt: new Date(this.#now().getTime() + token.expiresInMs),
    };
  }

  #tokenEndpoint(url: string): TokenEndpoint {
    return new TokenEndpoint({ providerName: 'Google', url, terminalErrorCodes: TerminalErrorCodes, fetch: this.#fetch });
  }

  #verifyScopes(scope: string): void {
    if (scope && !hasAllScopes(scope, RequiredGmailScopes)) throw new ReauthorizationRequiredError();
  }
}
