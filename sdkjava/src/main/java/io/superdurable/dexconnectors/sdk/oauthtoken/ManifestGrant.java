// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.JsonNodeFactory;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** One token grant resolved from a manifest authorization method, with manifest defaults applied. */
final class ManifestGrant {
    static final String REFRESH = "refresh";
    static final String CLIENT_CREDENTIALS = "client_credentials";
    static final String JWT_BEARER = "jwt_bearer";

    String kind;
    JsonNode endpoint;
    String tokenEndpointNetworkPolicy = "";
    JsonNode authentication = text("client_secret_post");
    String encoding = "form";
    String clientIdCredential = "";
    boolean shouldTrimClientId;
    String clientSecretCredential = "";
    boolean allowsPublicClient;
    String refreshField = "";
    List<JsonNode> mappings = Collections.emptyList();
    List<JsonNode> copies = Collections.emptyList();
    JsonNode scopes = JsonNodeFactory.instance.arrayNode();
    JsonNode requestScopes = JsonNodeFactory.instance.arrayNode();
    JsonNode acceptedScopeSets;
    JsonNode responseUrlValidation = JsonNodeFactory.instance.objectNode();
    String responseScopes = "check";
    String responseScopeField = "scope";
    String scopeResourcePrefix = "";
    boolean isCaseInsensitiveScopes;
    boolean isScopeResourcePrefixCaseInsensitive;
    String refreshWhenCredentialBlank = "";
    String expiryCap = "";
    JsonNode tokenExchangeTimeout;
    String accessTokenJwtExpiryFallback = "";
    boolean declaresAcceptedScopeSets;
    List<String> terminalCodes = Collections.emptyList();
    List<String> acceptedTokenTypes = Collections.emptyList();
    boolean isExactScopes;
    String scopeSeparators = "whitespace";
    boolean acceptsMissingTokenType;
    boolean acceptsMissingExpiresIn;
    JsonNode missingExpiryPolicy;
    boolean declaresMissingExpiry;
    JsonNode storedMissingExpiry = text("keep");
    String missingExpiryLifetime = "";
    Map<String, String> additional = Collections.emptyMap();
    String keyCredential = "";
    JsonNode assertion;

    private ManifestGrant() {
    }

    static List<ManifestGrant> fromMethod(final JsonNode method) {
        final JsonNode oauth = method.get("oauth2");
        if (isPresent(oauth)) {
            return fromOAuth(oauth);
        }
        final JsonNode client = method.get("clientCredentials");
        if (isPresent(client)) {
            return Collections.singletonList(fromClientCredentials(client));
        }
        final JsonNode serviceAccount = method.get("serviceAccount");
        final JsonNode bearer = serviceAccount == null ? null : serviceAccount.get("jwtBearer");
        if (isPresent(bearer)) {
            return Collections.singletonList(fromJwtBearer(bearer));
        }
        return Collections.emptyList();
    }

    String storedMissingExpiryText() {
        return storedMissingExpiry != null && storedMissingExpiry.isTextual() ? storedMissingExpiry.textValue() : "";
    }

    private static List<ManifestGrant> fromOAuth(final JsonNode oauth) {
        final JsonNode refresh = isPresent(oauth.get("refresh")) ? oauth.get("refresh") : JsonNodeFactory.instance.objectNode();
        List<JsonNode> declared = list(refresh.get("grants"));
        if (declared.isEmpty()) {
            String mapped = "";
            for (JsonNode item : list(oauth.get("credentialMappings"))) {
                if ("refresh_token".equals(item.path("source").asText(null)) && mapped.isEmpty()) {
                    mapped = item.path("credential").asText("");
                }
            }
            if (mapped.isEmpty()) {
                return Collections.emptyList();
            }
            final ObjectNode synthetic = JsonNodeFactory.instance.objectNode();
            synthetic.put("refreshTokenCredential", mapped);
            synthetic.set("credentialMappings", oauth.has("credentialMappings")
                    ? oauth.get("credentialMappings") : JsonNodeFactory.instance.arrayNode());
            declared = Collections.<JsonNode>singletonList(synthetic);
        }
        final List<ManifestGrant> grants = new ArrayList<>();
        for (JsonNode declaredGrant : declared) {
            final ManifestGrant grant = new ManifestGrant();
            grant.kind = REFRESH;
            grant.endpoint = required(oauth, "tokenEndpoint");
            grant.tokenEndpointNetworkPolicy = oauth.path("tokenEndpointNetworkPolicy").asText("");
            grant.authentication = oauth.has("tokenEndpointAuthMethod")
                    ? oauth.get("tokenEndpointAuthMethod") : text("client_secret_post");
            grant.encoding = oauth.path("tokenRequestEncoding").asText("form");
            grant.clientIdCredential = required(oauth, "clientIDCredential").asText();
            grant.shouldTrimClientId = refresh.path("trimClientID").asBoolean(false);
            grant.clientSecretCredential = required(oauth, "clientSecretCredential").asText();
            grant.allowsPublicClient = refresh.path("allowsPublicClient").asBoolean(false);
            grant.refreshField = required(declaredGrant, "refreshTokenCredential").asText();
            grant.mappings = list(required(declaredGrant, "credentialMappings"));
            if ("userScopes".equals(declaredGrant.path("grantedScopes").asText(null))) {
                grant.scopes = orEmptyArray(oauth.get("userScopes"));
            } else if (refresh.has("requiredScopes")) {
                grant.scopes = refresh.get("requiredScopes");
            } else {
                grant.scopes = orEmptyArray(oauth.get("scopes"));
            }
            applyCommon(grant, refresh, "invalid_grant");
            grant.responseUrlValidation = isPresent(refresh.get("responseURLValidation"))
                    ? refresh.get("responseURLValidation") : JsonNodeFactory.instance.objectNode();
            grant.responseScopeField = refresh.path("responseScopeField").asText("scope");
            grant.scopeResourcePrefix = refresh.path("scopeResourcePrefix").asText("");
            grant.isCaseInsensitiveScopes = refresh.path("caseInsensitiveScopes").asBoolean(false);
            grant.isScopeResourcePrefixCaseInsensitive =
                    refresh.path("scopeResourcePrefixCaseInsensitive").asBoolean(false);
            grant.refreshWhenCredentialBlank = refresh.path("refreshWhenCredentialBlank").asText("");
            grant.expiryCap = refresh.path("expiryCap").asText("");
            grant.accessTokenJwtExpiryFallback = refresh.path("accessTokenJWTExpiryFallback").asText("");
            grant.acceptedTokenTypes = firstNonEmptyStrings(declaredGrant.get("acceptedTokenTypes"),
                    refresh.get("acceptedTokenTypes"));
            grant.isExactScopes = declaredGrant.has("exactScopes")
                    ? declaredGrant.get("exactScopes").asBoolean(false)
                    : refresh.path("exactScopes").asBoolean(false);
            grant.acceptsMissingTokenType = declaredGrant.has("acceptsMissingTokenType")
                    ? declaredGrant.get("acceptsMissingTokenType").asBoolean(false)
                    : refresh.path("acceptsMissingTokenType").asBoolean(false);
            grant.additional = stringMap(refresh.get("additionalParameters"));
            grants.add(grant);
        }
        return grants;
    }

    private static ManifestGrant fromClientCredentials(final JsonNode client) {
        final ManifestGrant grant = new ManifestGrant();
        grant.kind = CLIENT_CREDENTIALS;
        grant.endpoint = required(client, "tokenEndpoint");
        grant.authentication = client.has("tokenEndpointAuthMethod")
                ? client.get("tokenEndpointAuthMethod") : text("client_secret_post");
        grant.encoding = client.path("tokenRequestEncoding").asText("form");
        grant.clientIdCredential = required(client, "clientIDCredential").asText();
        grant.shouldTrimClientId = client.path("trimClientID").asBoolean(false);
        grant.clientSecretCredential = required(client, "clientSecretCredential").asText();
        grant.mappings = list(required(client, "credentialMappings"));
        grant.scopes = orEmptyArray(client.get("scopes"));
        grant.requestScopes = orEmptyArray(client.get("scopes"));
        applyCommon(grant, client, "invalid_client");
        grant.acceptedTokenTypes = firstNonEmptyStrings(client.get("acceptedTokenTypes"), null);
        grant.acceptsMissingTokenType = client.path("acceptsMissingTokenType").asBoolean(false);
        grant.additional = stringMap(client.get("additionalParameters"));
        return grant;
    }

    private static ManifestGrant fromJwtBearer(final JsonNode bearer) {
        final ManifestGrant grant = new ManifestGrant();
        grant.kind = JWT_BEARER;
        grant.endpoint = required(bearer, "tokenEndpoint");
        grant.keyCredential = bearer.path("keyCredential").asText("");
        grant.assertion = bearer;
        grant.mappings = list(required(bearer, "credentialMappings"));
        grant.copies = list(bearer.get("credentialCopies"));
        grant.scopes = orEmptyArray(bearer.get("scopes"));
        applyCommon(grant, bearer, "invalid_grant");
        grant.responseUrlValidation = isPresent(bearer.get("responseURLValidation"))
                ? bearer.get("responseURLValidation") : JsonNodeFactory.instance.objectNode();
        grant.refreshWhenCredentialBlank = bearer.path("refreshWhenCredentialBlank").asText("");
        grant.acceptedTokenTypes = firstNonEmptyStrings(bearer.get("acceptedTokenTypes"), null);
        grant.acceptsMissingTokenType = bearer.path("acceptsMissingTokenType").asBoolean(false);
        grant.tokenExchangeTimeout = null;
        return grant;
    }

    private static void applyCommon(final ManifestGrant grant, final JsonNode declaration, final String defaultTerminalCode) {
        grant.acceptedScopeSets = declaration.get("acceptedScopeSets");
        grant.declaresAcceptedScopeSets = declaration.has("acceptedScopeSets");
        grant.responseScopes = declaration.path("responseScopes").asText("check");
        grant.tokenExchangeTimeout = declaration.get("tokenExchangeTimeout");
        final List<String> terminal = strings(declaration.get("terminalErrorCodes"));
        grant.terminalCodes = terminal.isEmpty() ? Collections.singletonList(defaultTerminalCode) : terminal;
        grant.scopeSeparators = declaration.path("scopeSeparators").asText("whitespace");
        final JsonNode missingExpiry = declaration.get("missingExpiry");
        grant.missingExpiryPolicy = missingExpiry;
        grant.declaresMissingExpiry = declaration.has("missingExpiry");
        grant.acceptsMissingExpiresIn = missingExpiry != null && !missingExpiry.isNull()
                && !(missingExpiry.isTextual() && "reject".equals(missingExpiry.textValue()));
        grant.storedMissingExpiry = declaration.has("storedMissingExpiry")
                ? declaration.get("storedMissingExpiry") : text("keep");
        grant.missingExpiryLifetime = missingExpiry != null && missingExpiry.isObject()
                ? missingExpiry.path("lifetime").asText("") : "";
    }

    private static List<String> firstNonEmptyStrings(final JsonNode first, final JsonNode second) {
        final List<String> values = strings(first);
        if (!values.isEmpty()) {
            return values;
        }
        final List<String> fallback = strings(second);
        return fallback.isEmpty() ? Collections.singletonList("Bearer") : fallback;
    }

    private static Map<String, String> stringMap(final JsonNode value) {
        final Map<String, String> result = new LinkedHashMap<>();
        if (value != null && value.isObject()) {
            final Iterator<Map.Entry<String, JsonNode>> fields = value.fields();
            while (fields.hasNext()) {
                final Map.Entry<String, JsonNode> field = fields.next();
                result.put(field.getKey(), field.getValue().asText());
            }
        }
        return result;
    }

    static List<JsonNode> list(final JsonNode value) {
        final List<JsonNode> result = new ArrayList<>();
        if (value != null && value.isArray()) {
            for (JsonNode item : value) {
                result.add(item);
            }
        }
        return result;
    }

    static List<String> strings(final JsonNode value) {
        final List<String> result = new ArrayList<>();
        for (JsonNode item : list(value)) {
            result.add(item.asText());
        }
        return result;
    }

    private static JsonNode orEmptyArray(final JsonNode value) {
        return value == null ? JsonNodeFactory.instance.arrayNode() : value;
    }

    private static JsonNode required(final JsonNode object, final String name) {
        final JsonNode value = object.get(name);
        if (value == null) {
            throw new IllegalArgumentException("manifest grant field " + name + " is required");
        }
        return value;
    }

    private static boolean isPresent(final JsonNode value) {
        if (value == null || value.isNull() || value.isMissingNode()) {
            return false;
        }
        if (value.isObject() || value.isArray()) {
            return value.size() > 0;
        }
        return !(value.isTextual() && value.textValue().isEmpty()) && !(value.isBoolean() && !value.booleanValue());
    }

    private static JsonNode text(final String value) {
        return JsonNodeFactory.instance.textNode(value);
    }
}
