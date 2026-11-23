// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.superdurable.dexconnectors.sdk.CredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.Failure;
import io.superdurable.dexconnectors.sdk.FailureKind;
import io.superdurable.dexconnectors.sdk.ReauthorizationRequiredException;
import io.superdurable.dexconnectors.sdk.RetryException;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.AddressPolicy;
import io.superdurable.dexconnectors.sdk.providerhttp.AddressRefusalException;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttp;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpClient;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.security.interfaces.RSAPrivateKey;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.function.Supplier;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.regex.PatternSyntaxException;

/**
 * Exchanges token grants described by a validated connector manifest, without owning storage.
 *
 * <p>{@code authorization} is the manifest's {@code spec.auth} JSON object. A
 * {@link CredentialFieldAccessor} maps its credential names onto the connector's credential type;
 * a multi-method manifest's credentials carry the selected method ID, and an empty ID selects
 * {@code defaultMethod}. The host owns the transport, refresh admission, and atomic replacement.
 *
 * <p>{@code storedMissingExpiry} controls undated stored tokens for every grant and defaults to
 * {@code keep}. The separate {@code missingExpiry} controls responses without {@code expires_in}.
 * Missing grant material and declared terminal provider errors throw
 * {@link ReauthorizationRequiredException}; transport and undeclared provider errors throw
 * {@link RetryException}. A refreshed credential without a lifetime expires at
 * {@link #NO_EXPIRY}.
 *
 * @param <C> the connector-specific credential type
 */
public final class ManifestCredentialRefreshDriver<C> implements CredentialRefreshDriver<C> {
    /** The Go zero time, used as the expiry of a refreshed credential without a lifetime. */
    public static final Instant NO_EXPIRY = Instant.parse("0001-01-01T00:00:00Z");

    private static final Pattern TEMPLATE_NAME = Pattern.compile("\\{([^{}]+)\\}");
    private static final Pattern WHITESPACE_SCOPES = Pattern.compile("\\s+", Pattern.UNICODE_CHARACTER_CLASS);
    private static final Pattern COMMA_SCOPES = Pattern.compile("[ ,]+");
    private static final Set<String> RESPONSE_URL_KEYS = new HashSet<>(Arrays.asList(
            "credential", "allowedHosts", "allowedHostSuffix", "allowAnyHTTPS", "allowLoopback",
            "forbidPort", "requireResponse", "refreshWhenInvalid", "defaultURL"));
    private static final Set<String> TEMPLATE_RULE_KEYS = new HashSet<>(Arrays.asList(
            "trimSpace", "maxBytes", "patterns", "reject", "lowercase"));
    private static final Duration MAXIMUM_EXCHANGE_TIMEOUT = Duration.ofHours(1);
    private static final ObjectMapper MAPPER = new ObjectMapper();

    private final String providerName;
    private final JsonNode authorization;
    private final CredentialFieldAccessor<C> fields;
    private final ProviderHttpTransport transport;
    private final Map<String, Object> settings;
    private final Supplier<Instant> now;

    /**
     * Configures a driver with manifest authorization, credential fields, and a caller-owned transport.
     *
     * @param providerName the provider name used in secret-safe errors
     * @param authorization the manifest {@code spec.auth} object
     * @param fields maps manifest credential names onto the credential type
     * @param transport the caller-owned provider HTTP transport
     * @param settings connection settings read by manifest value sources, as strings or integers
     * @param now supplies the issue time for expiry and JWT claims
     * @throws IllegalArgumentException if a required value is missing or a declaration is invalid
     */
    public ManifestCredentialRefreshDriver(
            final String providerName, final JsonNode authorization, final CredentialFieldAccessor<C> fields,
            final ProviderHttpTransport transport, final Map<String, Object> settings, final Supplier<Instant> now) {
        if (providerName == null || providerName.isEmpty() || authorization == null || authorization.size() == 0
                || fields == null || fields.getCredentialFieldNames().isEmpty() || transport == null || now == null) {
            throw new IllegalArgumentException("manifest refresh configuration and HTTP client are required");
        }
        this.providerName = providerName;
        this.authorization = authorization;
        this.fields = fields;
        this.transport = transport;
        this.settings = settings == null ? Collections.<String, Object>emptyMap() : new HashMap<>(settings);
        this.now = now;
        for (JsonNode method : declaredMethods()) {
            for (ManifestGrant grant : ManifestGrant.fromMethod(method)) {
                validateStoredMissingExpiry(grant);
                validateNewDeclarations(grant);
            }
        }
    }

    /**
     * Configures a driver without connection settings, using the system clock.
     *
     * @param providerName the provider name used in secret-safe errors
     * @param authorization the manifest {@code spec.auth} object
     * @param fields maps manifest credential names onto the credential type
     * @param transport the caller-owned provider HTTP transport
     */
    public ManifestCredentialRefreshDriver(
            final String providerName, final JsonNode authorization, final CredentialFieldAccessor<C> fields,
            final ProviderHttpTransport transport) {
        this(providerName, authorization, fields, transport, null, Instant::now);
    }

    /**
     * Checks declared access fields and the host's single credential expiry.
     *
     * @param state the current material, expiry, and decision time
     * @return {@code true} when the credential must be refreshed
     */
    @Override
    public boolean isRefreshRequired(final State<C> state) {
        final JsonNode method = selectedMethod(state.getCredentials());
        final List<ManifestGrant> grants = ManifestGrant.fromMethod(method);
        if (grants.isEmpty()) {
            return false;
        }
        final List<String> accessFields = new ArrayList<>();
        for (ManifestGrant grant : grants) {
            for (JsonNode mapping : grant.mappings) {
                if ("access_token".equals(mapping.path("source").asText(null))) {
                    accessFields.add(mapping.path("credential").asText());
                }
            }
        }
        boolean hasAccessToken = !accessFields.isEmpty();
        for (String name : accessFields) {
            hasAccessToken &= !credentialValue(state.getCredentials(), name).isEmpty();
        }
        MissingExpiryRule rule = MissingExpiryRule.KEEP_WHEN_EXPIRY_MISSING;
        for (ManifestGrant grant : grants) {
            if ("refresh".equals(grant.storedMissingExpiryText())) {
                rule = MissingExpiryRule.REFRESH_WHEN_EXPIRY_MISSING;
            }
        }
        for (ManifestGrant grant : grants) {
            if (!grant.refreshWhenCredentialBlank.isEmpty()
                    && credentialValue(state.getCredentials(), grant.refreshWhenCredentialBlank).isEmpty()) {
                return true;
            }
        }
        for (ManifestGrant grant : grants) {
            final JsonNode urlRule = grant.responseUrlValidation;
            if (urlRule.path("refreshWhenInvalid").asBoolean(false) && !GoText.isValidResponseUrl(
                    credentialValue(state.getCredentials(), urlRule.path("credential").asText("")), urlRule)) {
                return true;
            }
        }
        for (ManifestGrant grant : grants) {
            if ("refreshIfRefreshTokenPresent".equals(grant.storedMissingExpiryText())
                    && !credentialValue(state.getCredentials(), grant.refreshField).isEmpty()) {
                rule = MissingExpiryRule.REFRESH_WHEN_EXPIRY_MISSING;
            }
        }
        Instant effectiveExpiry = state.getExpiresAt();
        ManifestGrant jwtGrant = null;
        for (ManifestGrant grant : grants) {
            if (jwtGrant == null && !grant.accessTokenJwtExpiryFallback.isEmpty()) {
                jwtGrant = grant;
            }
        }
        if (jwtGrant != null && effectiveExpiry == null && !accessFields.isEmpty()) {
            final Long expirySeconds = GoText.readJwtExpirySeconds(
                    credentialValue(state.getCredentials(), accessFields.get(0)));
            if (expirySeconds != null) {
                effectiveExpiry = Instant.ofEpochSecond(expirySeconds);
            }
        }
        return OAuthRefreshRules.isRefreshRequired(hasAccessToken, effectiveExpiry, state.getNow(), rule, null);
    }

    /**
     * Exchanges each active grant and returns one complete replacement credential.
     *
     * @param state the current material, expiry, and decision time
     * @return the replacement credentials and their earliest expiry
     * @throws ReauthorizationRequiredException if grant material is missing or the provider
     *     reported a declared terminal error
     * @throws RetryException if the exchange failed in a retryable way
     * @throws AddressRefusalException if a private-network policy refused the endpoint
     */
    @Override
    public Result<C> refresh(final State<C> state) {
        C credentials = state.getCredentials();
        final JsonNode method = selectedMethod(credentials);
        final List<ManifestGrant> grants = ManifestGrant.fromMethod(method);
        if (grants.isEmpty()) {
            throw new ReauthorizationRequiredException();
        }
        for (ManifestGrant grant : grants) {
            validateGrant(grant);
        }
        final List<Instant> expiries = new ArrayList<>();
        for (ManifestGrant grant : grants) {
            if (!grant.refreshField.isEmpty() && credentialValue(credentials, grant.refreshField).isEmpty()) {
                continue;
            }
            final TokenResponse token;
            try {
                token = exchange(grant, credentials);
            } catch (TokenEndpoint.InvalidTokenEndpointUrlException error) {
                if ("privateNetworkHTTP".equals(grant.tokenEndpointNetworkPolicy)) {
                    throw new AddressRefusalException("URL is invalid");
                }
                throw protocolRetry("token endpoint is invalid");
            } catch (AddressRefusalException error) {
                throw error;
            } catch (IOException | IllegalArgumentException error) {
                throw protocolRetry(error.getMessage());
            }
            checkScopes(grant, token);
            final Map<String, String> updates = new LinkedHashMap<>();
            for (JsonNode mapping : grant.mappings) {
                final String destination = mapping.path("credential").asText();
                final String value = token.mappedResponseString(mapping.path("source").asText());
                if (value == null || (value.isEmpty() && destination.equals(grant.refreshField))) {
                    continue;
                }
                updates.put(destination, value);
            }
            final JsonNode urlRule = grant.responseUrlValidation;
            if (urlRule.size() > 0) {
                final String destination = urlRule.path("credential").asText();
                String source = "";
                for (JsonNode mapping : grant.mappings) {
                    if (source.isEmpty() && destination.equals(mapping.path("credential").asText())) {
                        source = mapping.path("source").asText();
                    }
                }
                final boolean hasResponse = token.getRetainedFields().containsKey(source);
                final JsonNode returned = token.getRetainedFields().get(source);
                final String prior = credentialValue(credentials, destination);
                String value;
                if (hasResponse) {
                    value = returned == null || returned.isNull() ? "" : returned.isTextual() ? returned.textValue() : null;
                } else if (urlRule.path("requireResponse").asBoolean(false)) {
                    value = null;
                } else {
                    value = prior.isEmpty() ? urlRule.path("defaultURL").asText("") : prior;
                }
                if (value == null || !GoText.isValidResponseUrl(value, urlRule)) {
                    throw protocolRetry("token response URL is invalid");
                }
                updates.put(destination, GoText.validateResponseUrl(value, urlRule));
            }
            for (JsonNode copy : grant.copies) {
                updates.put(copy.path("credential").asText(),
                        credentialValue(credentials, copy.path("fromCredential").asText()));
            }
            credentials = fields.withCredentialFields(credentials, updates);
            expiries.add(expiry(grant, token));
        }
        if (expiries.isEmpty()) {
            throw new ReauthorizationRequiredException();
        }
        return new Result<C>(credentials, Collections.min(expiries));
    }

    private void checkScopes(final ManifestGrant grant, final TokenResponse token) {
        List<String> requiredScopes = scopes(grant.scopes);
        if (ManifestGrant.REFRESH.equals(grant.kind)) {
            requiredScopes = withoutOfflineAccess(requiredScopes);
        }
        final Pattern separators = "whitespaceAndComma".equals(grant.scopeSeparators) ? COMMA_SCOPES : WHITESPACE_SCOPES;
        boolean hasReportedScope = !token.getScope().trim().isEmpty() && !isPythonBlank(token.getScope());
        List<String> reportedScopeArray = null;
        boolean hasScopeArray = true;
        if ("scopesArray".equals(grant.responseScopeField)) {
            final JsonNode rawScopes = token.getRetainedFields().get("scopes");
            hasScopeArray = rawScopes != null && !rawScopes.isNull();
            hasReportedScope = false;
            if (hasScopeArray) {
                if (!rawScopes.isArray()) {
                    throw new ReauthorizationRequiredException();
                }
                reportedScopeArray = new ArrayList<>();
                for (JsonNode scope : rawScopes) {
                    if (!scope.isTextual()) {
                        throw new ReauthorizationRequiredException();
                    }
                    reportedScopeArray.add(scope.textValue());
                }
                hasReportedScope = true;
            }
        }
        final List<String> reportedNames = reportedScopeArray != null ? reportedScopeArray
                : Arrays.asList(separators.split(token.getScope(), -1));
        final Set<String> grantedScopes = new HashSet<>();
        for (String scope : reportedNames) {
            grantedScopes.add(canonicalScope(grant, scope));
        }
        grantedScopes.remove("");
        List<List<String>> acceptedScopeSets = new ArrayList<>();
        if (grant.acceptedScopeSets != null && grant.acceptedScopeSets.isArray() && grant.acceptedScopeSets.size() > 0) {
            for (JsonNode scopeSet : grant.acceptedScopeSets) {
                acceptedScopeSets.add(ManifestGrant.strings(scopeSet));
            }
        } else {
            acceptedScopeSets.add(requiredScopes);
        }
        if (ManifestGrant.REFRESH.equals(grant.kind)) {
            final List<List<String>> filtered = new ArrayList<>();
            for (List<String> scopeSet : acceptedScopeSets) {
                filtered.add(withoutOfflineAccess(scopeSet));
            }
            acceptedScopeSets = filtered;
        }
        boolean isAccepted = false;
        for (List<String> scopeSet : acceptedScopeSets) {
            final Set<String> canonical = new HashSet<>();
            for (String scope : scopeSet) {
                canonical.add(canonicalScope(grant, scope));
            }
            if (grantedScopes.containsAll(canonical) && (!grant.isExactScopes || grantedScopes.equals(canonical))) {
                isAccepted = true;
            }
        }
        final boolean hasWrongScopes = (hasReportedScope || grant.isExactScopes) && !isAccepted;
        if (!"ignore".equals(grant.responseScopes) && hasScopeArray && hasWrongScopes) {
            throw new ReauthorizationRequiredException();
        }
    }

    private Instant expiry(final ManifestGrant grant, final TokenResponse token) {
        Duration lifetime = token.getExpiresIn();
        if (lifetime.isZero() || lifetime.isNegative()) {
            lifetime = GoDurations.parse(grant.missingExpiryLifetime);
        }
        if (!grant.expiryCap.isEmpty() && lifetime.compareTo(Duration.ZERO) > 0) {
            final Duration cap = GoDurations.parse(grant.expiryCap);
            lifetime = lifetime.compareTo(cap) < 0 ? lifetime : cap;
        }
        if (!grant.accessTokenJwtExpiryFallback.isEmpty()
                && (token.getExpiresIn().isZero() || token.getExpiresIn().isNegative())) {
            final Long expirySeconds = GoText.readJwtExpirySeconds(token.getAccessToken().reveal());
            final Instant issuedAt = now.get();
            final Instant jwtExpiry = expirySeconds == null ? null : Instant.ofEpochSecond(expirySeconds);
            lifetime = jwtExpiry != null && jwtExpiry.isAfter(issuedAt)
                    ? Duration.between(issuedAt, jwtExpiry)
                    : GoDurations.parse(grant.accessTokenJwtExpiryFallback);
        }
        return lifetime.isZero() ? NO_EXPIRY : now.get().plus(lifetime);
    }

    private TokenResponse exchange(final ManifestGrant grant, final C credentials) throws IOException {
        if ("privateNetworkHTTP".equals(grant.tokenEndpointNetworkPolicy) && transport instanceof ProviderHttpClient) {
            final Duration timeout = tokenExchangeTimeout(grant.tokenExchangeTimeout);
            return exchangeWithTransport(grant, credentials,
                    new ProviderHttpClient(timeout == null ? Duration.ofSeconds(30) : timeout,
                            AddressPolicy.PRIVATE_NETWORK_HTTP));
        }
        return exchangeWithTransport(grant, credentials, transport);
    }

    private TokenResponse exchangeWithTransport(
            final ManifestGrant grant, final C credentials, final ProviderHttpTransport selectedTransport)
            throws IOException {
        JsonNode key = MAPPER.createObjectNode();
        if (ManifestGrant.JWT_BEARER.equals(grant.kind) && !grant.keyCredential.isEmpty()) {
            key = parseServiceAccountKey(grant, credentials);
        }
        final String endpointUrl = resolveSource(grant.endpoint, credentials, key);
        final boolean isPrivateNetwork = "privateNetworkHTTP".equals(grant.tokenEndpointNetworkPolicy);
        if (isPrivateNetwork) {
            for (int index = 0; index < endpointUrl.length(); index++) {
                final char character = endpointUrl.charAt(index);
                if (character <= 0x20 || character > 0x7E || character == '#') {
                    throw new ReauthorizationRequiredException();
                }
            }
        }
        final List<String> retained = new ArrayList<>();
        if ("scopesArray".equals(grant.responseScopeField)) {
            retained.add("scopes");
        }
        final String urlCredential = grant.responseUrlValidation.path("credential").asText(null);
        for (JsonNode mapping : grant.mappings) {
            if (urlCredential != null && urlCredential.equals(mapping.path("credential").asText())) {
                retained.add(mapping.path("source").asText());
            }
        }
        final Duration timeout = ManifestGrant.JWT_BEARER.equals(grant.kind) ? null
                : tokenExchangeTimeout(grant.tokenExchangeTimeout);
        final TokenEndpoint endpoint = TokenEndpoint.newBuilder(providerName, endpointUrl, selectedTransport)
                .terminalErrorCodes(grant.terminalCodes)
                .allowsPlainHttp(isPrivateNetwork)
                .retainedResponseFields(retained)
                .acceptedTokenTypes(grant.acceptedTokenTypes)
                .acceptsMissingTokenType(grant.acceptsMissingTokenType)
                .acceptsMissingExpiresIn(grant.acceptsMissingExpiresIn)
                .usesJsonRequestBody("json".equals(grant.encoding))
                .additionalRequestParameters(grant.additional)
                .exchangeTimeout(timeout)
                .build();
        if (ManifestGrant.JWT_BEARER.equals(grant.kind)) {
            return endpoint.exchangeJwtBearerAssertion(signAssertion(grant, credentials, key));
        }
        final String secret = credentialValue(credentials, grant.clientSecretCredential);
        final boolean isRefresh = ManifestGrant.REFRESH.equals(grant.kind);
        if (secret.isEmpty() && isRefresh && !grant.allowsPublicClient) {
            throw new ReauthorizationRequiredException();
        }
        String methodName = resolveSource(grant.authentication, credentials, key);
        if (secret.isEmpty() && isRefresh && grant.allowsPublicClient) {
            methodName = "none";
        }
        final String clientId = credentialValue(credentials, grant.clientIdCredential);
        final ClientCredentials client = new ClientCredentials(
                grant.shouldTrimClientId ? GoText.trimSpace(clientId) : clientId, new SecretString(secret),
                ClientAuthenticationMethod.fromManifestValue(methodName));
        if (isRefresh) {
            return endpoint.exchangeRefreshToken(
                    client, new SecretString(credentialValue(credentials, grant.refreshField)));
        }
        return endpoint.exchangeClientCredentials(client, scopes(grant.requestScopes));
    }

    private JsonNode parseServiceAccountKey(final ManifestGrant grant, final C credentials) {
        try {
            final JsonNode parsed = MAPPER.readTree(
                    credentialValue(credentials, grant.keyCredential).getBytes(StandardCharsets.UTF_8));
            if (parsed == null || !parsed.isObject()) {
                throw new ReauthorizationRequiredException();
            }
            final JsonNode assertion = grant.assertion;
            for (JsonNode source : Arrays.asList(grant.endpoint, assertion.get("privateKey"), assertion.get("issuer"),
                    assertion.get("audience"), assertion.get("subject"))) {
                if (source != null && source.isObject() && source.has("fromKey")) {
                    final JsonNode value = parsed.get(source.get("fromKey").asText());
                    if (value != null && !value.isTextual()) {
                        throw new ReauthorizationRequiredException();
                    }
                }
            }
            return parsed;
        } catch (IOException | IllegalArgumentException error) {
            throw new ReauthorizationRequiredException();
        }
    }

    private SecretString signAssertion(final ManifestGrant grant, final C credentials, final JsonNode key) {
        final JsonNode assertion = grant.assertion;
        final Duration lifetime = GoDurations.parse(assertion.path("lifetime").asText(""));
        try {
            String subject = isTruthy(assertion.get("subject"))
                    ? resolveAssertionSource(assertion.get("subject"), assertion, credentials, key) : "";
            if ("goMailAddress".equals(assertion.path("subjectValidation").asText(null))) {
                subject = GoText.trimSpace(subject);
                if (!GoText.isGoMailAddress(subject)) {
                    throw new ReauthorizationRequiredException();
                }
            }
            final RSAPrivateKey privateKey = JwtBearerAssertion.parseRsaPrivateKeyPem(
                    resolveSource(required(assertion, "privateKey"), credentials, key));
            return new JwtBearerAssertion(
                    resolveAssertionSource(required(assertion, "issuer"), assertion, credentials, key),
                    resolveSource(required(assertion, "audience"), credentials, key),
                    now.get(), lifetime, subject, String.join(" ", scopes(assertion.get("scopes"))))
                    .sign(privateKey);
        } catch (IllegalArgumentException error) {
            throw new ReauthorizationRequiredException(error);
        }
    }

    private String resolveAssertionSource(
            final JsonNode source, final JsonNode assertion, final C credentials, final JsonNode key) {
        String value = resolveSource(source, credentials, key);
        if (source.isObject() && source.has("fromCredential")) {
            final String name = source.get("fromCredential").asText();
            for (JsonNode trimmed : assertion.path("trimSpaceSources")) {
                if (name.equals(trimmed.asText())) {
                    value = GoText.trimSpace(value);
                    if (value.isEmpty()) {
                        throw new ReauthorizationRequiredException();
                    }
                    break;
                }
            }
        }
        return value;
    }

    private String resolveSource(final JsonNode source, final C credentials, final JsonNode key) {
        if (source == null || source.isNull()) {
            throw new ReauthorizationRequiredException();
        }
        if (source.isTextual()) {
            return source.textValue();
        }
        if (!source.isObject()) {
            throw new ReauthorizationRequiredException();
        }
        if (source.has("value")) {
            return source.get("value").asText();
        }
        String value;
        if (source.has("fromCredential")) {
            value = credentialValue(credentials, source.get("fromCredential").asText());
        } else if (source.has("fromSetting")) {
            value = settingText(source.get("fromSetting").asText());
        } else if (source.has("fromKey")) {
            final JsonNode member = key.get(source.get("fromKey").asText());
            value = member == null ? "" : member.isTextual() ? member.textValue() : member.toString();
        } else if (source.has("template")) {
            return expandTemplate(source, credentials);
        } else {
            throw new ReauthorizationRequiredException();
        }
        if (source.has("values")) {
            final JsonNode values = source.get("values");
            if (!values.has(value)) {
                throw new ReauthorizationRequiredException();
            }
            return values.get(value).asText();
        }
        if (value.isEmpty()) {
            throw new ReauthorizationRequiredException();
        }
        return value;
    }

    private String expandTemplate(final JsonNode source, final C credentials) {
        String template = source.get("template").asText();
        final JsonNode rules = source.path("templateValueRules");
        final Matcher matcher = TEMPLATE_NAME.matcher(template);
        final List<String[]> replacements = new ArrayList<>();
        while (matcher.find()) {
            final String name = matcher.group(1);
            final String raw = fields.getCredentialFieldNames().contains(name)
                    ? credentialValue(credentials, name) : settingText(name);
            final String value = applyTemplateValueRule(raw, rules.has(name) ? rules.get(name) : MAPPER.createObjectNode());
            replacements.add(new String[] {matcher.group(0), ProviderHttp.percentEncode(value, ":@&=+$")});
        }
        for (String[] replacement : replacements) {
            template = template.replace(replacement[0], replacement[1]);
        }
        return template;
    }

    private String applyTemplateValueRule(final String raw, final JsonNode rule) {
        validateTemplateValueRule(rule);
        final String value = rule.path("trimSpace").asBoolean(false) ? GoText.trimSpace(raw) : raw;
        if (rule.has("maxBytes") && value.getBytes(StandardCharsets.UTF_8).length > rule.get("maxBytes").asLong()) {
            throw new ReauthorizationRequiredException();
        }
        for (JsonNode rejected : rule.path("reject")) {
            if (value.toLowerCase(Locale.ROOT).equals(rejected.asText().toLowerCase(Locale.ROOT))) {
                throw new ReauthorizationRequiredException();
            }
        }
        final JsonNode patterns = rule.path("patterns");
        if (patterns.size() > 0) {
            boolean isMatched = false;
            for (JsonNode pattern : patterns) {
                isMatched |= Pattern.compile(pattern.asText()).matcher(value).matches();
            }
            if (!isMatched) {
                throw new ReauthorizationRequiredException();
            }
        }
        return rule.path("lowercase").asBoolean(false) ? value.toLowerCase(Locale.ROOT) : value;
    }

    private String credentialValue(final C credentials, final String name) {
        if (name == null || name.isEmpty()) {
            return "";
        }
        if (!fields.getCredentialFieldNames().contains(name)) {
            throw new IllegalArgumentException("manifest credential field is not mapped");
        }
        final String value = fields.readCredentialField(credentials, name);
        return value == null ? "" : value;
    }

    private JsonNode selectedMethod(final C credentials) {
        final JsonNode methods = authorization.get("methods");
        if (methods == null || !methods.isArray() || methods.size() == 0) {
            return authorization;
        }
        String methodId = fields.readAuthMethodId(credentials);
        if (methodId == null) {
            throw new IllegalArgumentException("multi-method credentials require an auth method ID");
        }
        if (methodId.isEmpty()) {
            methodId = authorization.path("defaultMethod").asText("");
        }
        for (JsonNode method : methods) {
            if (methodId.equals(method.path("id").asText(null))) {
                return method;
            }
        }
        throw new ReauthorizationRequiredException();
    }

    private List<JsonNode> declaredMethods() {
        final JsonNode methods = authorization.get("methods");
        if (methods == null || !methods.isArray() || methods.size() == 0) {
            return Collections.singletonList(authorization);
        }
        return ManifestGrant.list(methods);
    }

    private void validateGrant(final ManifestGrant grant) {
        validateStoredMissingExpiry(grant);
        for (JsonNode mapping : grant.mappings) {
            if (mapping.path("source").asText("").isEmpty()
                    || !fields.getCredentialFieldNames().contains(mapping.path("credential").asText(""))) {
                throw new IllegalArgumentException("manifest credential field is not mapped");
            }
        }
        for (JsonNode copy : grant.copies) {
            if (!fields.getCredentialFieldNames().contains(copy.path("credential").asText(""))
                    || !fields.getCredentialFieldNames().contains(copy.path("fromCredential").asText(""))) {
                throw new IllegalArgumentException("manifest credential field is not mapped");
            }
        }
        if (!grant.missingExpiryLifetime.isEmpty()
                && GoDurations.parse(grant.missingExpiryLifetime).compareTo(Duration.ZERO) <= 0) {
            throw new IllegalArgumentException("manifest grant duration must be positive");
        }
        if (ManifestGrant.JWT_BEARER.equals(grant.kind)
                && GoDurations.parse(grant.assertion.path("lifetime").asText("")).compareTo(Duration.ofMillis(1)) < 0) {
            throw new IllegalArgumentException("manifest JWT assertion lifetime must be at least one millisecond");
        }
    }

    private static void validateStoredMissingExpiry(final ManifestGrant grant) {
        final String value = grant.storedMissingExpiryText();
        if (!"refresh".equals(value) && !"keep".equals(value) && !"refreshIfRefreshTokenPresent".equals(value)) {
            throw new IllegalArgumentException("manifest storedMissingExpiry must be refresh or keep");
        }
    }

    private void validateNewDeclarations(final ManifestGrant grant) {
        final JsonNode endpoint = grant.endpoint;
        if (endpoint != null && endpoint.isObject() && endpoint.has("templateValueRules")) {
            final JsonNode rules = endpoint.get("templateValueRules");
            if (!rules.isObject()) {
                throw new IllegalArgumentException("manifest template value rules are invalid");
            }
            for (JsonNode rule : rules) {
                validateTemplateValueRule(rule);
            }
        }
        final JsonNode urlRule = grant.responseUrlValidation;
        if (!urlRule.isObject()) {
            throw new IllegalArgumentException("manifest responseURLValidation field is invalid");
        }
        final Iterator<String> names = urlRule.fieldNames();
        while (names.hasNext()) {
            if (!RESPONSE_URL_KEYS.contains(names.next())) {
                throw new IllegalArgumentException("manifest responseURLValidation field is invalid");
            }
        }
        if (!grant.refreshWhenCredentialBlank.isEmpty()
                && !fields.getCredentialFieldNames().contains(grant.refreshWhenCredentialBlank)) {
            throw new IllegalArgumentException("manifest refreshWhenCredentialBlank field is not mapped");
        }
        if (urlRule.size() > 0) {
            final JsonNode credential = urlRule.get("credential");
            boolean isMapped = false;
            for (JsonNode mapping : grant.mappings) {
                isMapped |= credential != null && credential.isTextual()
                        && credential.textValue().equals(mapping.path("credential").asText(null));
            }
            if (credential == null || !credential.isTextual()
                    || !fields.getCredentialFieldNames().contains(credential.textValue()) || !isMapped) {
                throw new IllegalArgumentException("manifest responseURLValidation credential is not mapped");
            }
        }
        if (!grant.tokenEndpointNetworkPolicy.isEmpty() && !"privateNetworkHTTP".equals(grant.tokenEndpointNetworkPolicy)) {
            throw new IllegalArgumentException("manifest tokenEndpointNetworkPolicy is invalid");
        }
        if (!"scope".equals(grant.responseScopeField) && !"scopesArray".equals(grant.responseScopeField)) {
            throw new IllegalArgumentException("manifest responseScopeField is invalid");
        }
        if (!grant.scopeResourcePrefix.isEmpty() && !"https://graph.microsoft.com/".equals(grant.scopeResourcePrefix)) {
            throw new IllegalArgumentException("manifest scopeResourcePrefix is invalid");
        }
        if (!grant.accessTokenJwtExpiryFallback.isEmpty()
                && GoDurations.parse(grant.accessTokenJwtExpiryFallback).compareTo(Duration.ZERO) <= 0) {
            throw new IllegalArgumentException("manifest accessTokenJWTExpiryFallback must be positive");
        }
        if (!grant.expiryCap.isEmpty() && GoDurations.parse(grant.expiryCap).compareTo(Duration.ZERO) <= 0) {
            throw new IllegalArgumentException("manifest expiryCap must be positive");
        }
        tokenExchangeTimeout(grant.tokenExchangeTimeout);
        if (!"check".equals(grant.responseScopes) && !"ignore".equals(grant.responseScopes)) {
            throw new IllegalArgumentException("manifest responseScopes must be check or ignore");
        }
        final JsonNode policy = grant.missingExpiryPolicy;
        if (grant.declaresMissingExpiry && !isValidMissingExpiryPolicy(policy)) {
            throw new IllegalArgumentException("manifest missingExpiry must be reject, keep, or a positive lifetime");
        }
        if (!"whitespace".equals(grant.scopeSeparators) && !"whitespaceAndComma".equals(grant.scopeSeparators)) {
            throw new IllegalArgumentException("manifest scopeSeparators is invalid");
        }
        if (ManifestGrant.CLIENT_CREDENTIALS.equals(grant.kind) && grant.declaresAcceptedScopeSets) {
            throw new IllegalArgumentException("manifest clientCredentials does not support acceptedScopeSets");
        }
        if (grant.declaresAcceptedScopeSets && !isValidScopeSets(grant.acceptedScopeSets)) {
            throw new IllegalArgumentException("manifest acceptedScopeSets is invalid");
        }
        if (ManifestGrant.JWT_BEARER.equals(grant.kind)) {
            final JsonNode assertion = grant.assertion;
            if (assertion.has("subjectValidation") && !"goMailAddress".equals(assertion.get("subjectValidation").asText(null))) {
                throw new IllegalArgumentException("manifest subjectValidation is invalid");
            }
            if (assertion.has("subjectValidation") && !isTruthy(assertion.get("subject"))) {
                throw new IllegalArgumentException("manifest subjectValidation requires a subject");
            }
        }
    }

    Duration tokenExchangeTimeout(final JsonNode declaration) {
        if (declaration == null || declaration.isNull() || (declaration.isTextual() && declaration.textValue().isEmpty())) {
            return null;
        }
        Object value = declaration;
        if (declaration.isObject() && declaration.size() == 1 && declaration.has("fromSetting")) {
            value = settings.get(declaration.get("fromSetting").asText());
        }
        Duration timeout = Duration.ZERO;
        if (value instanceof JsonNode) {
            final JsonNode node = (JsonNode) value;
            if (node.isIntegralNumber()) {
                timeout = Duration.ofNanos(node.longValue());
            } else if (node.isTextual()) {
                timeout = GoDurations.parse(node.textValue());
            }
        } else if (value instanceof Integer || value instanceof Long) {
            timeout = Duration.ofNanos(((Number) value).longValue());
        } else if (value instanceof String) {
            timeout = GoDurations.parse((String) value);
        }
        if (timeout.isZero() || timeout.isNegative() || timeout.compareTo(MAXIMUM_EXCHANGE_TIMEOUT) > 0) {
            throw new IllegalArgumentException(
                    "manifest tokenExchangeTimeout must resolve to a positive duration at most 1h");
        }
        return timeout;
    }

    private List<String> scopes(final JsonNode source) {
        final List<String> result = new ArrayList<>();
        if (source == null) {
            return result;
        }
        if (source.isArray()) {
            for (JsonNode scope : source) {
                result.add(scope.asText());
            }
        } else if (source.isObject() && source.has("fromSetting")) {
            for (String scope : settingText(source.get("fromSetting").asText()).split("\\s+")) {
                if (!scope.isEmpty()) {
                    result.add(scope);
                }
            }
        }
        return result;
    }

    private String settingText(final String name) {
        final Object value = settings.get(name);
        return value == null ? "" : String.valueOf(value);
    }

    private String canonicalScope(final ManifestGrant grant, final String scope) {
        final String prefix = grant.scopeResourcePrefix;
        final String matchingScope = grant.isScopeResourcePrefixCaseInsensitive ? scope.toLowerCase(Locale.ROOT) : scope;
        final String matchingPrefix = grant.isScopeResourcePrefixCaseInsensitive ? prefix.toLowerCase(Locale.ROOT) : prefix;
        final String stripped = !prefix.isEmpty() && matchingScope.startsWith(matchingPrefix)
                ? scope.substring(prefix.length()) : scope;
        return grant.isCaseInsensitiveScopes ? stripped.toLowerCase(Locale.ROOT) : stripped;
    }

    private RetryException protocolRetry(final String message) {
        return new RetryException(new Failure(FailureKind.PROTOCOL, providerName, "refreshCredentials",
                message == null || message.isEmpty() ? "token exchange failed" : message));
    }

    private static List<String> withoutOfflineAccess(final List<String> scopes) {
        final List<String> result = new ArrayList<>(scopes);
        result.removeAll(Collections.singleton("offline_access"));
        return result;
    }

    private static boolean isValidMissingExpiryPolicy(final JsonNode policy) {
        if (policy != null && policy.isTextual()) {
            return "reject".equals(policy.textValue()) || "keep".equals(policy.textValue());
        }
        if (policy == null || !policy.isObject() || policy.size() != 1 || !policy.has("lifetime")
                || !policy.get("lifetime").isTextual()) {
            return false;
        }
        try {
            return GoDurations.parse(policy.get("lifetime").textValue()).compareTo(Duration.ZERO) > 0;
        } catch (IllegalArgumentException error) {
            return false;
        }
    }

    private static boolean isValidScopeSets(final JsonNode scopeSets) {
        if (scopeSets == null || !scopeSets.isArray() || scopeSets.size() == 0) {
            return false;
        }
        for (JsonNode scopeSet : scopeSets) {
            if (!scopeSet.isArray() || scopeSet.size() == 0) {
                return false;
            }
            for (JsonNode scope : scopeSet) {
                if (!scope.isTextual() || scope.textValue().isEmpty()) {
                    return false;
                }
            }
        }
        return true;
    }

    private static void validateTemplateValueRule(final JsonNode rule) {
        if (rule == null || !rule.isObject()) {
            throw new IllegalArgumentException("manifest template value rule is invalid");
        }
        final Iterator<String> names = rule.fieldNames();
        while (names.hasNext()) {
            if (!TEMPLATE_RULE_KEYS.contains(names.next())) {
                throw new IllegalArgumentException("manifest template value rule is invalid");
            }
        }
        if ((rule.has("trimSpace") && !rule.get("trimSpace").isBoolean())
                || (rule.has("lowercase") && !rule.get("lowercase").isBoolean())
                || (rule.has("maxBytes") && (!rule.get("maxBytes").isIntegralNumber() || rule.get("maxBytes").asLong() < 0))
                || (rule.has("reject") && !isStringArray(rule.get("reject")))
                || (rule.has("patterns") && !isStringArray(rule.get("patterns")))) {
            throw new IllegalArgumentException("manifest template value rule is invalid");
        }
        for (JsonNode pattern : rule.path("patterns")) {
            validatePortableTemplateValuePattern(pattern.textValue());
        }
    }

    private static void validatePortableTemplateValuePattern(final String pattern) {
        boolean isCharacterClass = false;
        for (int index = 0; index < pattern.length(); index++) {
            final char character = pattern.charAt(index);
            if (character == '\\') {
                index++;
                if (index < pattern.length() && "123456789pPQEk".indexOf(pattern.charAt(index)) >= 0) {
                    throw new IllegalArgumentException("manifest template value pattern is invalid");
                }
            } else if (character == '[') {
                if (isCharacterClass && index + 1 < pattern.length() && pattern.charAt(index + 1) == ':') {
                    throw new IllegalArgumentException("manifest template value pattern is invalid");
                }
                isCharacterClass = true;
            } else if (character == ']') {
                isCharacterClass = false;
            } else if (!isCharacterClass && character == '(' && index + 1 < pattern.length()
                    && pattern.charAt(index + 1) == '?'
                    && !(index + 2 < pattern.length() && pattern.charAt(index + 2) == ':')) {
                throw new IllegalArgumentException("manifest template value pattern is invalid");
            }
        }
        try {
            Pattern.compile(pattern);
        } catch (PatternSyntaxException error) {
            throw new IllegalArgumentException("manifest template value pattern is invalid");
        }
    }

    private static boolean isStringArray(final JsonNode value) {
        if (!value.isArray()) {
            return false;
        }
        for (JsonNode item : value) {
            if (!item.isTextual()) {
                return false;
            }
        }
        return true;
    }

    private static boolean isTruthy(final JsonNode value) {
        if (value == null || value.isNull()) {
            return false;
        }
        if (value.isTextual()) {
            return !value.textValue().isEmpty();
        }
        if (value.isContainerNode()) {
            return value.size() > 0;
        }
        return !value.isBoolean() || value.booleanValue();
    }

    private static boolean isPythonBlank(final String value) {
        for (int index = 0; index < value.length(); index++) {
            final char character = value.charAt(index);
            if (!Character.isWhitespace(character) && !Character.isSpaceChar(character)
                    && !(character >= 0x1c && character <= 0x1f) && character != 0x85) {
                return false;
            }
        }
        return true;
    }

    private static JsonNode required(final JsonNode object, final String name) {
        final JsonNode value = object.get(name);
        if (value == null) {
            throw new IllegalArgumentException("manifest JWT assertion " + name + " is required");
        }
        return value;
    }
}
