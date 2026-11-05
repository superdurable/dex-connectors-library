// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, RetryError } from '../errors.js';
import { isIP } from 'node:net';
import { isLoopbackIPAddressLiteral } from '../netpolicy.js';
import { SecretString, type CredentialRefreshDriver, type CredentialRefreshResult, type CredentialRefreshState } from '../credentials.js';
import {
  isRefreshRequired, parseRSAPrivateKeyPEM, signJWTBearerAssertion, TokenEndpoint,
  type ClientAuthenticationMethod, type TokenResponse,
} from './index.js';

type ManifestObject = Record<string, unknown>;
type GrantKind = 'refresh' | 'client_credentials' | 'jwt_bearer';

interface Grant {
  kind: GrantKind;
  endpoint: unknown;
  tokenEndpointNetworkPolicy: string;
  authentication: unknown;
  encoding: string;
  clientID: string;
  trimClientID: boolean;
  clientSecret: string;
  refreshField: string;
  keyCredential: string;
  assertion: ManifestObject;
  mappings: ManifestObject[];
  copies: ManifestObject[];
  scopes: unknown;
  acceptedScopeSets: unknown;
  responseScopes: unknown;
  responseScopeField: string;
  scopeResourcePrefix: string;
  caseInsensitiveScopes: boolean;
  scopeResourcePrefixCaseInsensitive: boolean;
  refreshWhenCredentialBlank: string;
  responseURLValidation: ManifestObject;
  expiryCap: string;
  accessTokenJWTExpiryFallback: string;
  requestScopes: unknown;
  terminalCodes: string[];
  acceptedTokenTypes: string[];
  exactScopes: boolean;
  scopeSeparators: 'whitespace' | 'whitespaceAndComma';
  allowsPublicClient: boolean;
  acceptsMissingTokenType: boolean;
  acceptsMissingExpiresIn: boolean;
  missingExpiryPolicy: unknown;
  storedMissingExpiry: unknown;
  missingExpiry: string;
  additionalParameters: Record<string, string>;
}

/** Manifest-backed OAuth and service-account grant options emitted by connectorctl. */
export interface ManifestCredentialRefreshOptions {
  /** Safe provider name used in token endpoint errors. */ providerName: string;
  /** Validated spec.auth declaration, encoded using the manifest JSON contract. */ auth: ManifestObject;
  /** Credential wire field name to connector runtime property. */ credentialFields: Readonly<Record<string, string>>;
  /** Startup-pinned connection settings that value sources may read. */ settings?: Readonly<Record<string, string>>;
  /** Caller-owned token HTTP transport. */ fetch?: typeof fetch;
  /** Clock for deterministic tests. */ now?: () => Date;
}

/** Exchange manifest-declared grants while a credential provider owns admission and storage.
 *
 * Stored missing expiry follows storedMissingExpiry, defaulting to keep. Refresh sends each active grant
 * and reports the earliest expiry. Declared sub-500 terminal errors require
 * reauthorization; other token endpoint failures remain retryable. A credential
 * with multiple methods uses the declared default when authMethodId is empty.
 */
export class ManifestCredentialRefreshDriver<C extends object> implements CredentialRefreshDriver<C> {
  readonly #providerName: string;
  readonly #auth: ManifestObject;
  readonly #credentialFields: Readonly<Record<string, string>>;
  readonly #settings: Readonly<Record<string, string>>;
  readonly #fetch: typeof fetch | undefined;
  readonly #now: () => Date;

  /** Bind validated auth data, pinned settings, a caller-owned transport, and a clock. */
  constructor(options: ManifestCredentialRefreshOptions) {
    if (!options?.providerName || !options.auth || !options.credentialFields) throw new TypeError('manifest refresh configuration is required');
    this.#providerName = options.providerName;
    this.#auth = options.auth;
    this.#credentialFields = options.credentialFields;
    this.#settings = options.settings ?? {};
    this.#fetch = options.fetch;
    this.#now = options.now ?? (() => new Date());
    const methods = manifestList(this.#auth.methods);
    for (const method of methods.length ? methods : [this.#auth]) {
      for (const grant of this.#grants(manifestObject(method))) {
        this.#validateStoredMissingExpiry(grant);
        this.#validateNewDeclarations(grant);
      }
    }
  }

  /** Check declared access fields against the host's single credential expiry. */
  refreshRequired(state: CredentialRefreshState<C>): boolean {
    const grants = this.#grants(this.#selectedMethod(state.credentials));
    if (!grants.length) return false;
    const accessFields = grants.flatMap(grant => grant.mappings.filter(mapping => mapping.source === 'access_token').map(mapping => manifestText(mapping.credential)));
    const hasAccessToken = accessFields.length > 0 && accessFields.every(name => Boolean(this.#credentialValue(state.credentials, name)));
    if (grants.some(grant => grant.refreshWhenCredentialBlank && !this.#credentialValue(state.credentials, grant.refreshWhenCredentialBlank))) return true;
    if (grants.some(grant => grant.responseURLValidation.refreshWhenInvalid === true &&
      !isValidResponseURL(this.#credentialValue(state.credentials, manifestText(grant.responseURLValidation.credential)), grant.responseURLValidation))) return true;
    const refreshWhenUndated = grants.some(grant => grant.storedMissingExpiry === 'refresh' ||
      (grant.storedMissingExpiry === 'refreshIfRefreshTokenPresent' && Boolean(this.#credentialValue(state.credentials, grant.refreshField))));
    const jwtFallback = grants.find(grant => grant.accessTokenJWTExpiryFallback);
    const storedJWTExpiry = jwtFallback && !state.expiresAt && accessFields.length
      ? readJWTExpirySeconds(this.#credentialValue(state.credentials, accessFields[0]!)) : undefined;
    const effectiveExpiry = storedJWTExpiry === undefined ? state.expiresAt : new Date(storedJWTExpiry * 1000);
    return isRefreshRequired(hasAccessToken, effectiveExpiry, state.now, refreshWhenUndated ? 'refresh' : 'keep');
  }

  /** Send each active grant and return one complete credential with the earliest expiry. */
  async refresh(state: CredentialRefreshState<C>, signal: AbortSignal): Promise<CredentialRefreshResult<C>> {
    let credentials = state.credentials;
    const method = this.#selectedMethod(credentials);
    const grants = this.#grants(method);
    if (!grants.length) throw new ReauthorizationRequiredError();
    for (const grant of grants) this.#validateGrant(grant);
    const expiries: number[] = [];
    for (const grant of grants) {
      if (grant.refreshField && !this.#credentialValue(credentials, grant.refreshField)) continue;
      let token: TokenResponse;
      try { token = await this.#exchange(grant, credentials, method, signal); }
      catch (error) {
        if (error instanceof ReauthorizationRequiredError) throw error;
        throw new RetryError({ kind: 'PROTOCOL', provider: this.#providerName, operation: 'refreshCredentials',
          message: error instanceof Error ? error.message : 'token endpoint failed' });
      }
      const requiredScopes = this.#scopes(grant.scopes).filter(scope => grant.kind !== 'refresh' || scope !== 'offline_access');
      const acceptedScopeSets = manifestList(grant.acceptedScopeSets).map(manifestList)
        .map(set => set.filter((scope): scope is string => typeof scope === 'string')
          .filter(scope => grant.kind !== 'refresh' || scope !== 'offline_access'));
      const scopeSets = acceptedScopeSets.length ? acceptedScopeSets : [requiredScopes];
      let reportedScopes: string | string[] = token.scope;
      let hasReportedScopes = reportedScopes.trim() !== '';
      if (grant.responseScopeField === 'scopesArray') {
        const returned = token.retainedFields.scopes;
        hasReportedScopes = false;
        if (returned !== undefined && returned !== null) {
          if (!Array.isArray(returned) || returned.some(value => typeof value !== 'string')) throw new ReauthorizationRequiredError();
          reportedScopes = returned as string[];
          hasReportedScopes = true;
        }
      }
      if (grant.responseScopes !== 'ignore' && !(grant.responseScopeField === 'scopesArray' && !hasReportedScopes) && (grant.exactScopes ? !scopeSets.some(set => hasDeclaredScopes(reportedScopes, set, grant.scopeSeparators, true, grant.scopeResourcePrefix, grant.caseInsensitiveScopes, grant.scopeResourcePrefixCaseInsensitive))
        : hasReportedScopes && !scopeSets.some(set => hasDeclaredScopes(reportedScopes, set, grant.scopeSeparators, false, grant.scopeResourcePrefix, grant.caseInsensitiveScopes, grant.scopeResourcePrefixCaseInsensitive)))) throw new ReauthorizationRequiredError();
      const updates: Record<string, unknown> = {};
      for (const mapping of grant.mappings) {
        const destination = manifestText(mapping.credential);
        const value = token.mappedCredentialValue(manifestText(mapping.source));
        if (value === undefined || (value === '' && destination === grant.refreshField)) continue;
        const property = this.#credentialFields[destination];
        if (!property) throw new TypeError('manifest credential field is not mapped');
        updates[property] = this.#isSecret(method, destination) ? new SecretString(value) : value;
      }
      if (grant.responseURLValidation.credential) {
        const destination = manifestText(grant.responseURLValidation.credential);
        const mapping = grant.mappings.find(item => item.credential === destination);
        const source = manifestText(mapping?.source);
        const hasResponse = Object.hasOwn(token.retainedFields, source);
        const returned = token.retainedFields[source];
        const prior = this.#credentialValue(credentials, destination);
        const value = hasResponse && returned !== null ? returned : returned === null ? prior
          : grant.responseURLValidation.requireResponse === true ? undefined : prior || manifestText(grant.responseURLValidation.defaultURL);
        if (typeof value !== 'string' || !isValidResponseURL(value, grant.responseURLValidation))
          throw new RetryError({ kind: 'PROTOCOL', provider: this.#providerName, operation: 'refreshCredentials', message: 'token response URL is invalid' });
        updates[this.#credentialFields[destination]!] = validateResponseURL(value, grant.responseURLValidation);
      }
      for (const copy of grant.copies) {
        const destination = manifestText(copy.credential);
        const property = this.#credentialFields[destination];
        if (!property) throw new TypeError('manifest credential field is not mapped');
        const value = this.#credentialValue(credentials, manifestText(copy.fromCredential));
        updates[property] = this.#isSecret(method, destination) ? new SecretString(value) : value;
      }
      credentials = { ...credentials, ...updates };
      const reportedLifetime = token.expiresInMs > 0 ? token.expiresInMs : parseDurationMs(grant.missingExpiry);
      let lifetime = grant.expiryCap && reportedLifetime > 0 ? Math.min(reportedLifetime, parseDurationMs(grant.expiryCap)) : reportedLifetime;
      if (grant.accessTokenJWTExpiryFallback && token.expiresInMs <= 0) {
        const expirySeconds = readJWTExpirySeconds(token.accessToken.reveal());
        const nowMilliseconds = this.#now().getTime();
        lifetime = expirySeconds !== undefined && expirySeconds * 1000 > nowMilliseconds
          ? expirySeconds * 1000 - nowMilliseconds : parseDurationMs(grant.accessTokenJWTExpiryFallback);
      }
      expiries.push(lifetime > 0 ? this.#now().getTime() + lifetime : 0);
    }
    if (!expiries.length) throw new ReauthorizationRequiredError();
    return { credentials, expiresAt: new Date(Math.min(...expiries)) };
  }

  #selectedMethod(credentials: C): ManifestObject {
    const methods = manifestList(this.#auth.methods);
    if (!methods.length) return this.#auth;
    const selected = manifestText((credentials as Record<string, unknown>).authMethodId);
    const method = methods.map(manifestObject).find(item => item.id === (selected || manifestText(this.#auth.defaultMethod)));
    if (!method) throw new ReauthorizationRequiredError();
    return method;
  }

  #grants(method: ManifestObject): Grant[] {
    if (method.oauth2) {
      const oauth = manifestObject(method.oauth2);
      const refresh = method.oauth2 && manifestObject(oauth.refresh);
      let declared = manifestList(refresh?.grants).map(manifestObject);
      if (!declared.length) {
        const mapped = manifestList(oauth.credentialMappings).map(manifestObject).find(mapping => mapping.source === 'refresh_token');
        if (!mapped) return [];
        declared = [{ refreshTokenCredential: mapped.credential, credentialMappings: oauth.credentialMappings }];
      }
      return declared.map(declaration => ({
        kind: 'refresh', endpoint: oauth.tokenEndpoint, tokenEndpointNetworkPolicy: manifestText(oauth.tokenEndpointNetworkPolicy), authentication: oauth.tokenEndpointAuthMethod ?? 'client_secret_post',
        encoding: manifestText(oauth.tokenRequestEncoding) || 'form', clientID: manifestText(oauth.clientIDCredential), trimClientID: refresh?.trimClientID === true,
        clientSecret: manifestText(oauth.clientSecretCredential), refreshField: manifestText(declaration.refreshTokenCredential),
        keyCredential: '', assertion: {}, mappings: manifestList(declaration.credentialMappings).map(manifestObject), copies: [],
        scopes: declaration.grantedScopes === 'userScopes' ? oauth.userScopes : (refresh?.requiredScopes ?? oauth.scopes), requestScopes: [],
        acceptedScopeSets: refresh?.acceptedScopeSets,
        responseURLValidation: manifestObject(refresh?.responseURLValidation),
        responseScopes: refresh?.responseScopes ?? 'check', responseScopeField: manifestText(refresh?.responseScopeField) || 'scope',
        scopeResourcePrefix: manifestText(refresh?.scopeResourcePrefix), caseInsensitiveScopes: refresh?.caseInsensitiveScopes === true, scopeResourcePrefixCaseInsensitive: refresh?.scopeResourcePrefixCaseInsensitive === true,
        refreshWhenCredentialBlank: manifestText(refresh?.refreshWhenCredentialBlank), expiryCap: manifestText(refresh?.expiryCap), accessTokenJWTExpiryFallback: manifestText(refresh?.accessTokenJWTExpiryFallback),
        terminalCodes: strings(refresh?.terminalErrorCodes, ['invalid_grant']),
        acceptedTokenTypes: strings(declaration.acceptedTokenTypes ?? refresh?.acceptedTokenTypes, ['Bearer']),
        exactScopes: declaration.exactScopes === true || (declaration.exactScopes === undefined && refresh?.exactScopes === true),
        scopeSeparators: (refresh?.scopeSeparators === undefined ? 'whitespace' : refresh.scopeSeparators) as Grant['scopeSeparators'],
        allowsPublicClient: refresh?.allowsPublicClient === true,
        acceptsMissingTokenType: declaration.acceptsMissingTokenType === undefined
          ? refresh?.acceptsMissingTokenType === true : declaration.acceptsMissingTokenType === true,
        acceptsMissingExpiresIn: refresh?.missingExpiry !== undefined && refresh.missingExpiry !== 'reject',
        missingExpiryPolicy: refresh?.missingExpiry,
        storedMissingExpiry: refresh?.storedMissingExpiry === undefined ? 'keep' : refresh.storedMissingExpiry,
        missingExpiry: missingExpiryLifetime(refresh?.missingExpiry), additionalParameters: stringRecord(refresh?.additionalParameters),
      }));
    }
    if (method.clientCredentials) {
      const client = manifestObject(method.clientCredentials);
      return [{
        kind: 'client_credentials', endpoint: client.tokenEndpoint, tokenEndpointNetworkPolicy: '',
        authentication: client.tokenEndpointAuthMethod ?? 'client_secret_post',
        encoding: manifestText(client.tokenRequestEncoding) || 'form', clientID: manifestText(client.clientIDCredential), trimClientID: client.trimClientID === true,
        clientSecret: manifestText(client.clientSecretCredential), refreshField: '', keyCredential: '', assertion: {},
        mappings: manifestList(client.credentialMappings).map(manifestObject), copies: [], scopes: client.scopes,
        requestScopes: client.scopes, acceptedScopeSets: client.acceptedScopeSets, terminalCodes: strings(client.terminalErrorCodes, ['invalid_client']),
        responseURLValidation: {},
        responseScopes: client.responseScopes ?? 'check', responseScopeField: 'scope', scopeResourcePrefix: '', caseInsensitiveScopes: false, scopeResourcePrefixCaseInsensitive: false,
        refreshWhenCredentialBlank: '', expiryCap: '', accessTokenJWTExpiryFallback: '',
        acceptedTokenTypes: strings(client.acceptedTokenTypes, ['Bearer']),
        exactScopes: false,
        scopeSeparators: (client.scopeSeparators ?? 'whitespace') as Grant['scopeSeparators'], allowsPublicClient: false,
        acceptsMissingTokenType: client.acceptsMissingTokenType === true,
        acceptsMissingExpiresIn: client.missingExpiry !== undefined && client.missingExpiry !== 'reject',
        missingExpiryPolicy: client.missingExpiry,
        storedMissingExpiry: client.storedMissingExpiry === undefined ? 'keep' : client.storedMissingExpiry,
        missingExpiry: missingExpiryLifetime(client.missingExpiry), additionalParameters: stringRecord(client.additionalParameters),
      }];
    }
    const serviceAccount = manifestObject(method.serviceAccount);
    if (!serviceAccount.jwtBearer) return [];
    const bearer = manifestObject(serviceAccount.jwtBearer);
    return [{
      kind: 'jwt_bearer', endpoint: bearer.tokenEndpoint, tokenEndpointNetworkPolicy: '', authentication: '', encoding: 'form',
      clientID: '', trimClientID: false, clientSecret: '', refreshField: '', keyCredential: manifestText(bearer.keyCredential),
      assertion: bearer, mappings: manifestList(bearer.credentialMappings).map(manifestObject),
      copies: manifestList(bearer.credentialCopies).map(manifestObject), scopes: bearer.scopes, requestScopes: [], acceptedScopeSets: bearer.acceptedScopeSets,
      responseURLValidation: manifestObject(bearer.responseURLValidation),
      responseScopes: bearer.responseScopes ?? 'check', responseScopeField: 'scope', scopeResourcePrefix: '', caseInsensitiveScopes: false, scopeResourcePrefixCaseInsensitive: false,
      refreshWhenCredentialBlank: manifestText(bearer.refreshWhenCredentialBlank), expiryCap: '', accessTokenJWTExpiryFallback: '',
      terminalCodes: strings(bearer.terminalErrorCodes, ['invalid_grant']),
      acceptedTokenTypes: strings(bearer.acceptedTokenTypes, ['Bearer']),
      exactScopes: false,
      scopeSeparators: (bearer.scopeSeparators === undefined ? 'whitespace' : bearer.scopeSeparators) as Grant['scopeSeparators'], allowsPublicClient: false,
      acceptsMissingTokenType: bearer.acceptsMissingTokenType === true,
      acceptsMissingExpiresIn: bearer.missingExpiry !== undefined && bearer.missingExpiry !== 'reject',
      missingExpiryPolicy: bearer.missingExpiry,
      storedMissingExpiry: bearer.storedMissingExpiry === undefined ? 'keep' : bearer.storedMissingExpiry,
      missingExpiry: missingExpiryLifetime(bearer.missingExpiry), additionalParameters: {},
    }];
  }

  async #exchange(grant: Grant, credentials: C, method: ManifestObject, signal: AbortSignal): Promise<TokenResponse> {
    let key: ManifestObject = {};
    if (grant.kind === 'jwt_bearer' && grant.keyCredential) {
      try { key = manifestObject(JSON.parse(this.#credentialValue(credentials, grant.keyCredential)) as unknown); }
      catch { throw new ReauthorizationRequiredError(); }
    }
    const endpointURL = this.#resolveSource(grant.endpoint, credentials, key);
    if (grant.tokenEndpointNetworkPolicy === 'privateNetworkHTTP' && (!/^[!-~]+$/.test(endpointURL) || endpointURL.includes('#')))
      throw new ReauthorizationRequiredError();
    const endpoint = new TokenEndpoint({
      providerName: this.#providerName, url: endpointURL,
      ...(grant.tokenEndpointNetworkPolicy === 'privateNetworkHTTP' ? { addressPolicy: 'privateNetworkHTTP' as const } : {}),
      terminalErrorCodes: grant.terminalCodes, acceptedTokenTypes: grant.acceptedTokenTypes,
      acceptsMissingTokenType: grant.acceptsMissingTokenType,
      acceptsMissingExpiresIn: grant.acceptsMissingExpiresIn, usesJSONRequestBody: grant.encoding === 'json',
      additionalRequestParameters: grant.additionalParameters,
      credentialMappingPaths: grant.mappings.map(mapping => manifestText(mapping.source)),
      retainedResponseFields: [...(grant.responseScopeField === 'scopesArray' ? ['scopes'] : []),
        ...grant.mappings.filter(mapping => mapping.credential === grant.responseURLValidation.credential).map(mapping => manifestText(mapping.source))],
      ...(this.#fetch ? { fetch: this.#fetch } : {}),
    });
    if (grant.kind === 'jwt_bearer') {
      const assertion = grant.assertion;
      const lifetimeMs = parseDurationMs(manifestText(assertion.lifetime));
      let signed: SecretString;
      try {
        const subject = assertion.subject ? this.#resolveAssertionSource(assertion.subject, assertion, credentials, key) : '';
        const validatedSubject = assertion.subjectValidation === 'goMailAddress' ? trimGoSpace(subject) : subject;
        if (assertion.subjectValidation === 'goMailAddress' && !isGoMailAddress(validatedSubject))
          throw new ReauthorizationRequiredError();
        signed = signJWTBearerAssertion({
          issuer: this.#resolveAssertionSource(assertion.issuer, assertion, credentials, key),
          audience: this.#resolveSource(assertion.audience, credentials, key),
          ...(assertion.subject ? { subject: validatedSubject } : {}),
          scope: this.#scopes(assertion.scopes).join(' '), issuedAt: this.#now(),
          lifetimeMs,
        }, parseRSAPrivateKeyPEM(this.#resolveSource(assertion.privateKey, credentials, key)));
      } catch { throw new ReauthorizationRequiredError(); }
      return await endpoint.exchangeJWTBearerAssertion(signed, signal);
    }
    const secret = this.#credentialValue(credentials, grant.clientSecret);
    if (!secret && !grant.allowsPublicClient && (grant.kind === 'refresh' || this.#isRequired(method, grant.clientSecret)))
      throw new ReauthorizationRequiredError();
    const authenticationMethod = secret || grant.kind === 'client_credentials'
      ? this.#resolveSource(grant.authentication, credentials, key) as ClientAuthenticationMethod : 'none';
    const clientID = this.#credentialValue(credentials, grant.clientID);
    const client = { id: grant.trimClientID ? trimGoSpace(clientID) : clientID, secret: new SecretString(secret), authenticationMethod };
    if (grant.kind === 'refresh') {
      return endpoint.exchangeRefreshToken(client, new SecretString(this.#credentialValue(credentials, grant.refreshField)), signal);
    }
    return endpoint.exchangeClientCredentials(client, this.#scopes(grant.requestScopes), signal);
  }

  #resolveAssertionSource(source: unknown, assertion: ManifestObject, credentials: C, key: ManifestObject): string {
    const value = this.#resolveSource(source, credentials, key);
    const field = manifestText(manifestObject(source).fromCredential);
    if (!manifestList(assertion.trimSpaceSources).includes(field)) return value;
    const trimmed = trimGoSpace(value);
    if (!trimmed) throw new ReauthorizationRequiredError();
    return trimmed;
  }

  #credentialValue(credentials: C, name: string): string {
    const property = this.#credentialFields[name];
    if (!property) return '';
    const value: unknown = (credentials as Record<string, unknown>)[property];
    return value instanceof SecretString ? value.reveal() : typeof value === 'string' ? value : '';
  }

  #isSecret(method: ManifestObject, name: string): boolean {
    return manifestList(method.fields).map(manifestObject)
      .some(field => field.name === name && field.type === 'secretString');
  }

  #isRequired(method: ManifestObject, name: string): boolean {
    const field = manifestList(method.fields).map(manifestObject)
      .find(candidate => candidate.name === name);
    return field ? field.required === true : true;
  }

  #validateGrant(grant: Grant): void {
    this.#validateStoredMissingExpiry(grant);
    if (grant.tokenEndpointNetworkPolicy && grant.tokenEndpointNetworkPolicy !== 'privateNetworkHTTP') throw new TypeError('manifest tokenEndpointNetworkPolicy is invalid');
    for (const mapping of grant.mappings) {
      if (!manifestText(mapping.source) || !this.#credentialFields[manifestText(mapping.credential)])
        throw new TypeError('manifest credential field is not mapped');
    }
    for (const copy of grant.copies) {
      if (!this.#credentialFields[manifestText(copy.credential)] || !this.#credentialFields[manifestText(copy.fromCredential)])
        throw new TypeError('manifest credential field is not mapped');
    }
    if (grant.missingExpiry && parseDurationMs(grant.missingExpiry) <= 0)
      throw new TypeError('manifest grant duration must be positive');
    if (grant.kind === 'jwt_bearer' && parseDurationMs(manifestText(grant.assertion.lifetime)) < 1)
      throw new TypeError('manifest JWT assertion lifetime must be at least one millisecond');
  }

  #validateNewDeclarations(grant: Grant): void {
    const urlRule = grant.responseURLValidation;
    const allowedURLKeys = new Set(['credential', 'allowedHosts', 'allowedHostSuffix', 'allowAnyHTTPS', 'allowLoopback', 'forbidPort', 'requireResponse', 'refreshWhenInvalid', 'defaultURL']);
    if (Object.keys(urlRule).some(key => !allowedURLKeys.has(key))) throw new TypeError('manifest responseURLValidation field is invalid');
    if (grant.refreshWhenCredentialBlank && !this.#credentialFields[grant.refreshWhenCredentialBlank])
      throw new TypeError('manifest refreshWhenCredentialBlank field is not mapped');
    if (Object.keys(urlRule).length && (!manifestText(urlRule.credential) || !this.#credentialFields[manifestText(urlRule.credential)] ||
      !grant.mappings.some(mapping => mapping.credential === urlRule.credential)))
      throw new TypeError('manifest responseURLValidation credential is not mapped');
    if (grant.responseScopeField !== 'scope' && grant.responseScopeField !== 'scopesArray') throw new TypeError('manifest responseScopeField is invalid');
    if (grant.scopeResourcePrefix && grant.scopeResourcePrefix !== 'https://graph.microsoft.com/') throw new TypeError('manifest scopeResourcePrefix is invalid');
    if (grant.expiryCap && parseDurationMs(grant.expiryCap) <= 0) throw new TypeError('manifest expiryCap must be positive');
    if (grant.accessTokenJWTExpiryFallback && parseDurationMs(grant.accessTokenJWTExpiryFallback) <= 0) throw new TypeError('manifest accessTokenJWTExpiryFallback must be positive');
    if (grant.responseScopes !== 'check' && grant.responseScopes !== 'ignore')
      throw new TypeError('manifest responseScopes must be check or ignore');
    const policy = grant.missingExpiryPolicy;
    if (policy !== undefined && policy !== 'reject' && policy !== 'keep' &&
      !(Object.keys(manifestObject(policy)).length === 1 && typeof manifestObject(policy).lifetime === 'string'
        && manifestText(manifestObject(policy).lifetime) !== ''))
      throw new TypeError('manifest missingExpiry must be reject, keep, or a positive lifetime');
    if (typeof manifestObject(policy).lifetime === 'string' && parseDurationMs(manifestText(manifestObject(policy).lifetime)) <= 0)
      throw new TypeError('manifest missingExpiry lifetime must be positive');
    if (grant.kind === 'jwt_bearer' && grant.assertion.subjectValidation !== undefined && !grant.assertion.subject)
      throw new TypeError('manifest subjectValidation requires a subject');
    if (grant.scopeSeparators !== 'whitespace' && grant.scopeSeparators !== 'whitespaceAndComma')
      throw new TypeError('manifest scopeSeparators is invalid');
    if (grant.kind === 'client_credentials' && grant.acceptedScopeSets !== undefined)
      throw new TypeError('manifest clientCredentials does not support acceptedScopeSets');
    if (grant.acceptedScopeSets !== undefined && (!Array.isArray(grant.acceptedScopeSets)
      || grant.acceptedScopeSets.length === 0 || grant.acceptedScopeSets.some((set: unknown) =>
        !Array.isArray(set) || set.length === 0 || set.some(scope => typeof scope !== 'string' || !scope))))
      throw new TypeError('manifest acceptedScopeSets is invalid');
    if (grant.kind === 'jwt_bearer' && grant.assertion.subjectValidation !== undefined
      && grant.assertion.subjectValidation !== 'goMailAddress')
      throw new TypeError('manifest subjectValidation is invalid');
  }

  #validateStoredMissingExpiry(grant: Grant): void {
    if (grant.storedMissingExpiry !== 'refresh' && grant.storedMissingExpiry !== 'keep' && grant.storedMissingExpiry !== 'refreshIfRefreshTokenPresent')
      throw new TypeError('manifest storedMissingExpiry must be refresh or keep');
  }

  #scopes(source: unknown): string[] {
    if (Array.isArray(source)) return source.filter((value): value is string => typeof value === 'string');
    const fromSetting = manifestObject(source).fromSetting;
    return typeof fromSetting === 'string' ? (this.#settings[fromSetting] ?? '').split(/\s+/).filter(Boolean) : [];
  }

  #resolveSource(source: unknown, credentials: C, key: ManifestObject): string {
    if (typeof source === 'string') return source;
    const valueSource = manifestObject(source);
    if (typeof valueSource.value === 'string') return valueSource.value;
    if (typeof valueSource.template === 'string') return valueSource.template.replace(/\{([^{}]+)\}/g, (_, name: string) => {
      const raw = this.#credentialFields[name] ? this.#credentialValue(credentials, name) : this.#settings[name] ?? '';
      const rule = manifestObject(manifestObject(valueSource.templateValueRules)[name]);
      const value = applyTemplateValueRule(raw, rule);
      return pathEscape(value);
    });
    let value = '';
    if (typeof valueSource.fromCredential === 'string') value = this.#credentialValue(credentials, valueSource.fromCredential);
    else if (typeof valueSource.fromSetting === 'string') value = this.#settings[valueSource.fromSetting] ?? '';
    else if (typeof valueSource.fromKey === 'string') value = manifestText(key[valueSource.fromKey]);
    else throw new ReauthorizationRequiredError();
    if (valueSource.values) {
      const translated = manifestObject(valueSource.values)[value];
      if (typeof translated !== 'string') throw new ReauthorizationRequiredError();
      return translated;
    }
    if (!value) throw new ReauthorizationRequiredError();
    return value;
  }
}

function manifestObject(value: unknown): ManifestObject {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as ManifestObject : {};
}
function manifestList(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function manifestText(value: unknown): string { return typeof value === 'string' ? value : ''; }
function strings(value: unknown, fallback: string[]): string[] {
  const found = manifestList(value).filter((item): item is string => typeof item === 'string');
  return found.length ? found : fallback;
}
function stringRecord(value: unknown): Record<string, string> {
  return Object.fromEntries(Object.entries(manifestObject(value)).filter((entry): entry is [string, string] => typeof entry[1] === 'string'));
}
function missingExpiryLifetime(value: unknown): string { return manifestText(manifestObject(value).lifetime); }
function hasDeclaredScopes(returnedScopes: string | readonly string[], requiredScopes: readonly string[], separators: Grant['scopeSeparators'], exact: boolean, prefix: string, ignoreCase: boolean, ignorePrefixCase: boolean): boolean {
  const canonical = (scope: string): string => { const stripped = prefix && (ignorePrefixCase ? scope.toLowerCase().startsWith(prefix.toLowerCase()) : scope.startsWith(prefix)) ? scope.slice(prefix.length) : scope; return ignoreCase ? stripped.toLowerCase() : stripped; };
  const returnedNames = typeof returnedScopes === 'string' ? returnedScopes.split(separators === 'whitespaceAndComma' ? /[ ,]+/ : /\s+/) : returnedScopes;
  const granted = new Set(returnedNames.filter(Boolean).map(canonical));
  const required = new Set(requiredScopes.map(canonical));
  return (!exact || granted.size === required.size) && [...required].every(scope => granted.has(scope));
}

function trimGoSpace(value: string): string {
  const isSpace = (code: number): boolean => code === 0x20 || (code >= 0x09 && code <= 0x0d)
    || code === 0x85 || code === 0xa0 || code === 0x1680 || (code >= 0x2000 && code <= 0x200a)
    || code === 0x2028 || code === 0x2029 || code === 0x202f || code === 0x205f || code === 0x3000;
  const characters = Array.from(value);
  while (characters.length && isSpace(characters[0]!.codePointAt(0)!)) characters.shift();
  while (characters.length && isSpace(characters.at(-1)!.codePointAt(0)!)) characters.pop();
  return characters.join('');
}

// Go 1.24 net/mail address equality leaves only its addr-spec grammar.
function isGoMailAddress(value: string): boolean {
  const at = value.indexOf('@');
  if (at <= 0 || at !== value.lastIndexOf('@') || at === value.length - 1) return false;
  const local = value.slice(0, at);
  const domain = value.slice(at + 1);
  const isAtom = (text: string): boolean => {
    if (!text || text.startsWith('.') || text.endsWith('.') || text.includes('..')) return false;
    for (const character of text) {
      const code = character.codePointAt(0)!;
      if (code >= 0xd800 && code <= 0xdfff) return false;
      if (code >= 0x80) continue;
      if (code < 0x21 || code > 0x7e || '()<>[]:;@\\,"'.includes(character)) return false;
    }
    return true;
  };
  if (!isAtom(local)) return false;
  if (domain.startsWith('[') && domain.endsWith(']')) {
    const address = domain.slice(1, -1);
    return !address.includes('%') && isIP(address) !== 0;
  }
  return isAtom(domain);
}
function pathEscape(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`)
    .replace(/%(?:2B|26|3D|3A|40|24)/g, escaped => ({ '%2B': '+', '%26': '&', '%3D': '=', '%3A': ':', '%40': '@', '%24': '$' })[escaped] ?? escaped);
}
function applyTemplateValueRule(raw: string, rule: ManifestObject): string {
  let value = rule.trimSpace === true ? trimGoSpace(raw) : raw;
  if (typeof rule.maxBytes === 'number' && Buffer.byteLength(value, 'utf8') > rule.maxBytes)
    throw new ReauthorizationRequiredError();
  if (manifestList(rule.reject).some(rejected => typeof rejected === 'string' && rejected.toLowerCase() === value.toLowerCase()))
    throw new ReauthorizationRequiredError();
  const patterns = manifestList(rule.patterns);
  if (patterns.length && !patterns.some(pattern => {
    if (typeof pattern !== 'string') throw new TypeError('manifest template value pattern is invalid');
    const matched = new RegExp(pattern).exec(value);
    return matched?.index === 0 && matched[0].length === value.length;
  })) throw new ReauthorizationRequiredError();
  if (rule.lowercase === true) value = value.toLowerCase();
  return value;
}
function parseDurationMs(value: string): number {
  if (!value) return 0;
  if (value === '0' || value === '+0' || value === '-0') return 0;
  const sign = value[0] === '-' ? -1n : 1n;
  const magnitude = value[0] === '+' || value[0] === '-' ? value.slice(1) : value;
  const parts = magnitude.matchAll(/((?:\d+(?:\.\d*)?|\.\d+))(ns|us|µs|μs|ms|s|m|h)/g);
  const nanoseconds: Record<string, bigint> = {
    ns: 1n, us: 1_000n, 'µs': 1_000n, 'μs': 1_000n,
    ms: 1_000_000n, s: 1_000_000_000n, m: 60_000_000_000n, h: 3_600_000_000_000n,
  };
  let duration = 0n; let position = 0;
  for (const part of parts) {
    if (part.index !== position) throw new TypeError('manifest grant duration is invalid');
    const [whole, fraction = ''] = part[1]!.split('.');
    const unit = nanoseconds[part[2]!]!;
    duration += BigInt(whole || '0') * unit;
    if (fraction) duration += BigInt(fraction) * unit / 10n ** BigInt(fraction.length);
    position += part[0].length;
  }
  if (position !== magnitude.length || duration === 0n
    || duration > (sign < 0n ? 9_223_372_036_854_775_808n : 9_223_372_036_854_775_807n))
    throw new TypeError('manifest grant duration is invalid');
  return Number(sign * duration) / 1_000_000;
}

function readJWTExpirySeconds(token: string): number | undefined {
  if (!token || Buffer.byteLength(token) > 16 << 10) return undefined;
  const segments = token.split('.');
  if (segments.length !== 3) return undefined;
  if (!/^[A-Za-z0-9_-]*=*$/.test(segments[1]!)) return undefined;
  try {
    const claims: unknown = JSON.parse(Buffer.from(segments[1]!.replace(/=+$/, ''), 'base64url').toString('utf8'));
    const exp = manifestObject(claims).exp;
    return typeof exp === 'number' && Number.isFinite(exp) && exp > 0 && exp <= 0x7fffffff * 4 ? Math.trunc(exp) : undefined;
  } catch { return undefined; }
}

function isValidResponseURL(value: string, rule: ManifestObject): boolean {
  try { validateResponseURL(value, rule); return true; } catch { return false; }
}
function validateResponseURL(value: string, rule: ManifestObject): string {
  if (!value || !/^[!-~]+$/.test(value) || /[?#]/.test(value)) throw new TypeError('response URL is invalid');
  if (!value.startsWith('https://') && !value.startsWith('http://')) throw new TypeError('response URL is invalid');
  const parsed = new URL(value);
  if (parsed.username || parsed.password || (parsed.pathname !== '' && parsed.pathname !== '/') || parsed.search || parsed.hash)
    throw new TypeError('response URL is invalid');
  const host = parsed.hostname.toLowerCase();
  const authority = value.slice(value.indexOf('://') + 3).split('/')[0]!;
  const rawHost = authority.startsWith('[') ? authority.slice(1, authority.indexOf(']')) : authority.split(':')[0]!;
  const hasExplicitPort = authority.startsWith('[') ? authority.slice(authority.indexOf(']') + 1).startsWith(':') : authority.includes(':');
  const loopback = rawHost.toLowerCase() === 'localhost' || isLoopbackIPAddressLiteral(rawHost);
  if (parsed.protocol !== 'https:' && !(rule.allowLoopback === true && loopback && parsed.protocol === 'http:')) throw new TypeError('response URL is invalid');
  if (rule.forbidPort === true && hasExplicitPort) throw new TypeError('response URL is invalid');
  const hosts = manifestList(rule.allowedHosts);
  const suffix = manifestText(rule.allowedHostSuffix);
  if (!(rule.allowAnyHTTPS === true && parsed.protocol === 'https:') &&
    !(rule.allowLoopback === true && loopback) && !hosts.includes(host) &&
    !(suffix && (host === suffix || host.endsWith(`.${suffix}`)))) throw new TypeError('response URL is invalid');
  return value.replace(/\/+$/, '');
}
