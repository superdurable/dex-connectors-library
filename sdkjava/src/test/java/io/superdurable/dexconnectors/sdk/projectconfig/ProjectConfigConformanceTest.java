// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.MAPPER;
import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.assertJsonValueEquals;
import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.load;
import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.ReauthorizationRequiredException;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.nio.file.attribute.PosixFilePermissions;
import java.time.Duration;
import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Comparator;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Iterator;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.stream.Stream;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

class ProjectConfigConformanceTest {
    private Path temporaryDirectory;

    @BeforeEach
    void createTemporaryDirectory() throws IOException {
        temporaryDirectory = Files.createTempDirectory(Paths.get("/tmp"), "sdkjava-projectconfig-");
    }

    @AfterEach
    void deleteTemporaryDirectory() throws IOException {
        try (Stream<Path> paths = Files.walk(temporaryDirectory)) {
            paths.sorted(Comparator.reverseOrder()).forEach(path -> path.toFile().delete());
        }
    }

    @Test
    void matchesGoCanonicalSnapshotAndCredentialBytes() throws Exception {
        final JsonNode fixture = load("project-storage");
        final Configuration configuration = Configuration.fromJson(fixture.get("configurationSnapshotDocument"));
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final ConfigurationStore configurationStore = new ConfigurationStore(objects, configuration.getScope());
        final byte[] snapshotBytes = configurationStore.writeConfiguration(0, configuration).getObject().getContents();
        assertEquals(fixture.get("configurationSnapshotCanonicalJSON").textValue(),
                new String(snapshotBytes, StandardCharsets.UTF_8));
        assertEquals(fixture.get("snapshot").get("digest").textValue(), GoCompatibleJson.digestBytes(snapshotBytes));
        assertEquals(fixture.get("snapshot"), configurationStore.freezeConfiguration(1).toJson());
        assertEquals(fixture.get("snapshot"), SnapshotRef.fromJson(fixture.get("snapshot")).toJson());
        assertEquals(fixture.get("configurationSnapshotCanonicalJSON").textValue(),
                text(GoCompatibleJson.encode(fixture.get("configurationSnapshotDocument"))));
        assertJsonValueEquals(fixture.get("configurationSnapshotDocument"), configuration.toJson());

        final ConnectionKey connectionKey = new ConnectionKey(
                fixture.get("connectionKey").get("connectorId").textValue(),
                fixture.get("connectionKey").get("connectionName").textValue());
        assertEquals(fixture.get("connectionHeadKey").textValue(), connectionKey.path(configuration.getScope()) + "/head");
        final JsonNode credentialFixture = fixture.get("credentialEnvelope");
        final Rfc3339NanoTimestamp timestamp = Rfc3339NanoTimestamp.fromWire(credentialFixture.get("expiresAt").textValue());
        final ConnectionStore connectionStore = new ConnectionStore(objects, configuration.getScope());
        connectionStore.replaceCredential(connectionKey, 0, new CredentialMaterial(
                (ObjectNode) credentialFixture.get("credentials"), timestamp, credentialFixture.get("authMethod").textValue()));
        final String credentialKey = fixture.get("credentialObjectKey").textValue();
        final byte[] credentialBytes = objects.readObject(credentialKey, "").getContents();
        assertEquals(fixture.get("credentialEnvelopeCanonicalJSON").textValue(), text(credentialBytes));
        assertEquals(fixture.get("objectReference").get("digest").textValue(), GoCompatibleJson.sha256Hex(credentialBytes));
        assertEquals(fixture.get("objectReference").get("key").textValue(), objects.readObject(credentialKey, "").getKey());
        assertEquals(credentialFixture.get("expiresAt").textValue(),
                connectionStore.readCredentialMaterial(connectionKey).getMaterial().getExpiresAt().toWire());

        final ObjectNode exchangeEnvelope = ConnectionDocuments.credentialEnvelope(configuration.getScope(), connectionKey,
                2, (ObjectNode) credentialFixture.get("credentials"), timestamp, "oauth2", "attempt-7");
        assertEquals(fixture.get("credentialExchangeEnvelopeCanonicalJSON").textValue(),
                text(GoCompatibleJson.encode(exchangeEnvelope)));
        final ObjectNode reversedCredentials = GoCompatibleJson.newObject();
        reversedCredentials.put("refreshToken", "REDACTED_REFRESH_TOKEN");
        reversedCredentials.put("accessToken", "REDACTED_ACCESS_TOKEN");
        // Go sorts credential map keys, as the TypeScript SDK does.
        assertTrue(text(GoCompatibleJson.encode(ConnectionDocuments.credentialEnvelope(configuration.getScope(),
                connectionKey, 2, reversedCredentials, timestamp, "oauth2", "")))
                .contains("\"accessToken\":\"REDACTED_ACCESS_TOKEN\",\"refreshToken\""));

        final JsonNode recordFixture = fixture.get("connectionRecord");
        final byte[] connectionRecordBytes = GoCompatibleJson.encode(ConnectionDocuments.connectionRecord(
                configuration.getScope(), connectionKey, recordFixture.get("revision").longValue(),
                recordFixture.get("fence").longValue(), recordFixture.get("status").textValue(), timestamp,
                recordFixture.get("authMethod").textValue(), fixture.get("objectReference"), fixture.get("mutation")));
        assertEquals(fixture.get("connectionRecordCanonicalJSON").textValue(), text(connectionRecordBytes));
        assertEquals(fixture.get("objectCAS").get("etag").textValue(),
                GoCompatibleJson.sha256Hex(connectionRecordBytes) + "-" + fixture.get("objectCAS").get("version").textValue());
        assertEquals(fixture.get("connectionHeadKey"), fixture.get("objectCAS").get("key"));
        final InMemoryObjectStore casObjects = new InMemoryObjectStore();
        final ProjectObject initial = casObjects.createObject(fixture.get("connectionHeadKey").textValue(),
                "{}".getBytes(StandardCharsets.UTF_8));
        assertEquals(fixture.get("objectCAS").get("etag").textValue(), casObjects.compareAndSwapObject(
                fixture.get("connectionHeadKey").textValue(), initial.getEtag(), connectionRecordBytes).getEtag());

        final JsonNode completed = fixture.get("completedConnectionRecord");
        assertEquals(completed, MAPPER.readTree(GoCompatibleJson.encode(ConnectionDocuments.connectionRecord(
                configuration.getScope(), connectionKey, completed.get("revision").longValue(),
                completed.get("fence").longValue(), completed.get("status").textValue(), timestamp,
                completed.get("authMethod").textValue(), completed.get("credential"), null))));
        assertEquals(fixture.get("exchangeResultKey"), completed.get("credential").get("key"));
        assertEquals(GoCompatibleJson.sha256Hex(fixture.get("credentialExchangeEnvelopeCanonicalJSON").textValue()
                .getBytes(StandardCharsets.UTF_8)), completed.get("credential").get("digest").textValue());
        assertEquals(fixture.get("mutation"), fixture.get("connectionRecord").get("mutation"));
        assertEquals(fixture.get("exchangeResultKey"), fixture.get("mutation").get("resultKey"));
        assertEquals(fixture.get("connectionHeadKey").textValue().replaceAll("/head$",
                "/credentials/" + fixture.get("objectReference").get("digest").textValue()), credentialKey);

        final ApplicationSecretRef secretReference = ApplicationSecretRef.fromJson(fixture.get("applicationSecret"));
        ProjectConfigurationRules.validateSecretReference(secretReference, configuration.getScope(),
                ProjectConfigurationRules.CREDENTIAL_BROKER_SECRET_NAME);
        assertArrayEquals(GoCompatibleJson.encode(fixture.get("applicationSecret")),
                GoCompatibleJson.encode(secretReference.toJson()));
        assertEquals(secretReference, configuration.getCredentialBroker().getTokenSecretRef());
        assertEquals(MAPPER.readTree("{\"schemaVersion\":\"connectors.dex.dev/project-trigger-inbox/v1alpha1\","
                + "\"events\":[]}"), fixture.get("emptyTriggerInboxDocument"));
        assertEquals(fixture.get("triggerInboxDocument"),
                MAPPER.readTree(GoCompatibleJson.encode(fixture.get("triggerInboxDocument"))));
        assertTrue(fixture.get("triggerInboxHeadKey").textValue()
                .startsWith(configuration.getScope().prefix() + "/trigger-inboxes/sample/"));
        assertEquals(configuration.getScope().prefix() + "/trigger-inboxes/sample/"
                + ConnectionKey.base64Url(connectionKey.getConnectionName()) + "/itemCreated/"
                + ConnectionKey.base64Url("main / west") + "/head", fixture.get("triggerInboxHeadKey").textValue());
    }

    @Test
    void canonicalizesTimestampFieldsLikeTypeScript() throws Exception {
        final JsonNode fixture = load("project-storage");
        final String canonical = fixture.get("credentialEnvelopeCanonicalJSON").textValue();
        final ObjectNode envelope = (ObjectNode) fixture.get("credentialEnvelope").deepCopy();
        envelope.put("expiresAt", "2026-10-29T12:30:45.120+00:00");
        assertEquals(canonical.replace("2026-10-29T12:30:45.123456789+08:00", "2026-10-29T12:30:45.12Z"),
                text(GoCompatibleJson.encode(envelope)));
        envelope.put("expiresAt", "2026-10-29T12:30:45.000Z");
        assertTrue(text(GoCompatibleJson.encode(envelope)).contains("\"expiresAt\":\"2026-10-29T12:30:45Z\""));
        envelope.put("expiresAt", "2026-10-29T12:30:45-00:00");
        assertTrue(text(GoCompatibleJson.encode(envelope)).contains("\"expiresAt\":\"2026-10-29T12:30:45Z\""));
        envelope.put("expiresAt", "2026-10-29T12:30:45.120+08:00");
        assertTrue(text(GoCompatibleJson.encode(envelope)).contains("\"expiresAt\":\"2026-10-29T12:30:45.12+08:00\""));
        final ObjectNode record = (ObjectNode) fixture.get("connectionRecord").deepCopy();
        ((ObjectNode) record.get("mutation")).put("deadline", "2026-10-29T12:30:45+00:00");
        assertTrue(text(GoCompatibleJson.encode(record)).contains("\"deadline\":\"2026-10-29T12:30:45Z\""));
        final ObjectNode numbers = GoCompatibleJson.newObject();
        numbers.putObject("configuration").put("ratio", 1.0).put("small", 1e-7).put("large", 1e21).put("count", 12);
        assertEquals("{\"configuration\":{\"count\":12,\"large\":1e+21,\"ratio\":1,\"small\":1e-7}}",
                text(GoCompatibleJson.encode(numbers)));
    }

    @Test
    void writesZeroOffsetCredentialsWithGoUtcBytes() throws Exception {
        final Scope scope = new Scope("project-1", "preview");
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final ConnectionStore store = new ConnectionStore(objects, scope);
        for (String[] variant : new String[][] {{"positive", "+00:00"}, {"negative", "-00:00"}}) {
            final ConnectionKey key = new ConnectionKey("sample", variant[0]);
            final Rfc3339NanoTimestamp supplied = Rfc3339NanoTimestamp.fromWire("2026-10-29T12:30:45.120" + variant[1]);
            final Rfc3339NanoTimestamp expected = Rfc3339NanoTimestamp.fromWire("2026-10-29T12:30:45.12Z");
            store.replaceCredential(key, 0, new CredentialMaterial(token("REDACTED"), supplied, "oauth2"));
            final byte[] expectedEnvelope = GoCompatibleJson.encode(ConnectionDocuments.credentialEnvelope(scope, key, 1,
                    token("REDACTED"), expected, "oauth2", ""));
            final String credentialKey = key.path(scope) + "/credentials/" + GoCompatibleJson.sha256Hex(expectedEnvelope);
            assertArrayEquals(expectedEnvelope, objects.readObject(credentialKey, "").getContents());
            final JsonNode record = MAPPER.readTree(objects.readObject(key.path(scope) + "/head", "").getContents());
            assertEquals("2026-10-29T12:30:45.12Z", record.get("expiresAt").textValue());
        }
    }

    @Test
    void pinsSnapshotsWithRevisionCasAndExactVersion() throws Exception {
        final Scope scope = new Scope("project-1", "preview");
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final ConfigurationStore store = new ConfigurationStore(objects, scope);
        final Configuration document = Configuration.newBuilder(scope, 1)
                .addConnection(new ConnectionConfiguration("sample", "main", "module", "sample", GoCompatibleJson.newObject()))
                .build();
        final Configuration first = store.writeConfiguration(0, document).getConfiguration();
        final SnapshotRef frozen = store.freezeConfiguration(first.getRevision());
        assertEquals(2, store.writeConfiguration(1, document).getConfiguration().getRevision());
        assertEquals(1, store.readSnapshot(frozen).getRevision());
        assertThrows(ObjectConflictException.class, () -> store.writeConfiguration(1, document));
        assertThrows(IllegalArgumentException.class, () -> store.readSnapshot(new SnapshotRef(frozen.getKey(),
                frozen.getVersion(), "sha256:" + repeat('0', 64))));
    }

    @Test
    void replacesPrivateMaterialWithRevisionCas() throws Exception {
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final Scope scope = new Scope("project-1", "live");
        final ConnectionStore store = new ConnectionStore(objects, scope);
        final ConnectionKey key = new ConnectionKey("sample", "main / west");
        assertEquals(1, store.replaceCredential(key, 0,
                new CredentialMaterial(apiKey("inert"), null, "apiKey")).getRevision());
        final ConnectionStore.LoadedMaterial loaded = store.readCredentialMaterial(key);
        assertEquals(apiKey("inert"), loaded.getMaterial().getCredentials());
        assertFalse((loaded.getMaterial().toString() + loaded.getConnection()).contains("inert"));
        assertThrows(ObjectConflictException.class, () -> store.replaceCredential(key, 0, loaded.getMaterial()));
        assertEquals(2, store.replaceCredential(key, 1, new CredentialMaterial(apiKey("new"), null, "apiKey")).getFence());
        assertTrue(key.path(scope).endsWith("/bWFpbiAvIHdlc3Q"));
    }

    @Test
    void rejectsForeignPrivateReference() throws Exception {
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final Scope scope = new Scope("project-1", "preview");
        final ConnectionStore store = new ConnectionStore(objects, scope);
        final ConnectionKey key = new ConnectionKey("sample", "main");
        store.replaceCredential(key, 0, new CredentialMaterial(apiKey("inert"), null, "apiKey"));
        final ProjectObject head = objects.readObject(key.path(scope) + "/head", "");
        final ObjectNode record = (ObjectNode) MAPPER.readTree(head.getContents());
        ((ObjectNode) record.get("credential")).put("key",
                "projects/other/preview/connections/sample/foreign/credentials/secret");
        objects.compareAndSwapObject(head.getKey(), head.getEtag(), MAPPER.writeValueAsBytes(record));
        assertThrows(IllegalArgumentException.class, () -> store.readCredentialMaterial(key));
    }

    @Test
    void localDexWebFilesFailFastInThisRelease() {
        final Map<String, String> local = new HashMap<>();
        local.put(ProjectConfigurationLoader.LOCAL_CONNECTIONS_FILE_ENVIRONMENT,
                temporaryDirectory.resolve("connections.json").toString());
        final UnsupportedOperationException error = assertThrows(UnsupportedOperationException.class,
                () -> ProjectConfigurationLoader.loadFromEnvironment(local, null));
        assertEquals("Java SDK 0.1.0 supports hosted project configuration (broker credentials, S3 object store "
                + "supplied by the application) and static credentials only; local Dex Web files are not supported yet",
                error.getMessage());
    }

    @Test
    void resolvesPinnedApplicationSecretsAndRejectsReservedNames() throws Exception {
        final Scope scope = new Scope("project-1", "preview");
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final ConfigurationStore store = new ConfigurationStore(objects, scope);
        final String secret = "inert<&>  secret";
        final ApplicationSecretRef reference = store.createApplicationSecret("WEBHOOK_SECRET", secret);
        final Configuration document = Configuration.newBuilder(scope, 1)
                .putEnvironment("WEBHOOK_SECRET", EnvironmentValue.ofSecret(reference)).build();
        final Configuration written = store.writeConfiguration(0, document).getConfiguration();
        final SnapshotRef snapshot = store.freezeConfiguration(written.getRevision());
        final LoadedProject project = ProjectConfigurationLoader.loadProjectFromObjectStore(objects, scope, snapshot, null);
        assertEquals(java.util.Collections.singletonMap("WEBHOOK_SECRET", secret), project.resolveApplicationEnvironment());
        final ProjectObject privateObject = objects.readObject(reference.getKey(), reference.getVersion());
        assertTrue(text(privateObject.getContents()).contains("\\u003c\\u0026\\u003e\\u2028\\u2029"));
        assertEquals(reference.getDigest(), GoCompatibleJson.digestBytes(privateObject.getContents()));
        assertThrows(IllegalArgumentException.class, () -> store.createApplicationSecret("DEX_CONFIG_KEY", "bad"));
        assertThrows(IllegalArgumentException.class, () -> store.readSecretValue(reference, "OTHER_SECRET"));
        assertThrows(IllegalArgumentException.class, () -> Configuration.newBuilder(scope, 1)
                .putEnvironment("WEBHOOK_SECRET", EnvironmentValue.ofSecret(new ApplicationSecretRef(
                        reference.getKey().replace("project-1", "other"), reference.getVersion(), reference.getDigest())))
                .build().validate());
    }

    @Test
    void rejectsDuplicateOperationAndPublicHttpBroker() {
        final Scope scope = new Scope("project-1", "preview");
        final ConnectionConfiguration entry =
                new ConnectionConfiguration("sample", "main", "module", "sample", GoCompatibleJson.newObject());
        final ObjectNode operation = GoCompatibleJson.newObject();
        operation.put("connectorId", "sample").put("connectionName", "main").put("operationId", "getItem")
                .put("flowType", "ExampleFlow").put("stepType", "GetItem").putObject("configuration");
        assertThrows(IllegalArgumentException.class, () -> Configuration.newBuilder(scope, 1).addConnection(entry)
                .addOperationConfiguration(operation).addOperationConfiguration(operation).build().validate());
        final ApplicationSecretRef token = new ApplicationSecretRef(
                scope.prefix() + "/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/" + repeat('0', 32), "version-1",
                "sha256:" + repeat('0', 64));
        assertThrows(IllegalArgumentException.class, () -> Configuration.newBuilder(scope, 1).addConnection(entry)
                .credentialBroker(new CredentialBrokerConfiguration("http://public.example/read", token)).build().validate());
        Configuration.newBuilder(scope, 1).addConnection(entry)
                .credentialBroker(new CredentialBrokerConfiguration("http://10.0.0.7/read", token)).build().validate();
    }

    @Test
    void renewsRotatedBrokerTokenAndRejectsSameRevision() throws Exception {
        final JsonNode fixture = load("credential-broker");
        final List<JsonNode> requests = new ArrayList<>();
        final BrokerHttpTransport transport = (url, bearerToken, body, maximumResponseBytes) -> {
            requests.add(MAPPER.readTree(body));
            if ("old".equals(bearerToken)) {
                return new BrokerHttpTransport.Response(401, null);
            }
            final ObjectNode ready = fixture.get("ready").deepCopy();
            ready.put("expiresAt", OffsetDateTime.now(ZoneOffset.UTC).plusHours(1).toString());
            return new BrokerHttpTransport.Response(200, MAPPER.writeValueAsBytes(ready));
        };
        final CredentialBrokerClient broker = new CredentialBrokerClient(
                new CredentialBrokerConfiguration("https://broker.example/read",
                        new ApplicationSecretRef("token", "old", "sha256:" + repeat('0', 64))),
                reference -> "new".equals(reference.getVersion()) ? "new" : "old",
                () -> new ApplicationSecretRef("token", "new", "sha256:" + repeat('0', 64)), transport);
        final SharedConnection shared = new SharedConnection("shared-7", broker);
        assertEquals("REDACTED_ACCESS_TOKEN", shared.resolve("call-1").get("accessToken").textValue());
        assertEquals(4, broker.read("shared-7", 3).getRevision());
        assertThrows(RejectedCredentialNotRefreshedException.class, () -> shared.resolveAfterRejection("call-1"));
        assertEquals(fixture.get("firstRequest"), requests.get(0));
        assertTrue(requests.contains(fixture.get("refreshRequest")));
        assertEquals(MAPPER.readTree("{\"sharedConnectionId\":\"shared-7\",\"rejectedRevision\":4}"),
                requests.get(requests.size() - 1));
        final BrokerCredential ready = CredentialBrokerClient.decode(
                new BrokerHttpTransport.Response(200, MAPPER.writeValueAsBytes(fixture.get("ready"))));
        assertEquals(ZoneOffset.ofHours(8), ready.getExpiresAt().getOffset());
        assertEquals(123456789, ready.getExpiresAt().getNano());
        assertFalse(ready.toString().contains("REDACTED_ACCESS_TOKEN"));
        assertThrows(ReauthorizationRequiredException.class, () -> CredentialBrokerClient.decode(
                new BrokerHttpTransport.Response(200, MAPPER.writeValueAsBytes(fixture.get("reauthorizationRequired")))));
        assertThrows(ConnectionNotSelectedException.class, () -> CredentialBrokerClient.decode(
                new BrokerHttpTransport.Response(200, MAPPER.writeValueAsBytes(fixture.get("connectionNotSelected")))));
        assertEquals(MAPPER.readTree("{\"sharedConnectionId\":\"shared-7\",\"rejectedRevision\":3}"),
                fixture.get("refreshRequest"));
    }

    @Test
    void classifiesBrokerStatusesAndInvalidAnswers() throws Exception {
        final ApplicationSecretRef reference = new ApplicationSecretRef("token", "v1", "sha256:" + repeat('0', 64));
        final CredentialBrokerConfiguration configuration =
                new CredentialBrokerConfiguration("https://broker.example/read", reference);
        final Map<Integer, Class<? extends Exception>> statuses = new HashMap<>();
        statuses.put(503, CredentialBrokerUnavailableException.class);
        statuses.put(400, CredentialBrokerRejectedException.class);
        statuses.put(401, CredentialBrokerRejectedException.class);
        for (Map.Entry<Integer, Class<? extends Exception>> status : statuses.entrySet()) {
            final CredentialBrokerClient broker = new CredentialBrokerClient(configuration, ignored -> "inert",
                    () -> reference, (url, token, body, maximum) -> new BrokerHttpTransport.Response(status.getKey(), null));
            assertThrows(status.getValue(), () -> broker.read("shared-7", 0));
        }
        for (String answer : new String[] {"{\"status\":\"READY\",\"credential\":{},\"revision\":0}",
            "{\"status\":\"UNKNOWN\",\"secret\":\"inert\"}"}) {
            final CredentialBrokerClient broker = new CredentialBrokerClient(configuration, ignored -> "inert",
                    () -> reference, (url, token, body, maximum) ->
                    new BrokerHttpTransport.Response(200, answer.getBytes(StandardCharsets.UTF_8)));
            assertThrows(CredentialBrokerRejectedException.class, () -> broker.read("shared-7", 0));
        }
        final byte[] oversized = new byte[(1 << 20) + 1];
        Arrays.fill(oversized, (byte) 'x');
        final CredentialBrokerClient broker = new CredentialBrokerClient(configuration, ignored -> "inert",
                () -> reference, (url, token, body, maximum) -> new BrokerHttpTransport.Response(200, oversized));
        assertThrows(CredentialBrokerRejectedException.class, () -> broker.read("shared-7", 0));
    }

    @Test
    void loadsGoProjectEnvironmentSelectors() throws Exception {
        final JsonNode environment = load("project-environment");
        final JsonNode storage = load("project-storage");
        assertEquals("DEX_CREDENTIAL_BROKER_TOKEN", environment.get("credentialBrokerSecretName").textValue());
        assertEquals(ProjectConfigurationRules.CREDENTIAL_BROKER_SECRET_NAME,
                environment.get("credentialBrokerSecretName").textValue());
        assertEquals(storage.get("snapshot").get("key"), environment.get("hosted").get("DEX_PROJECT_CONFIG_KEY"));
        assertEquals(new HashSet<>(Arrays.asList("DEX_PROJECT_CONFIG_DIGEST", "DEX_PROJECT_CONFIG_KEY",
                "DEX_PROJECT_CONFIG_VERSION", "DEX_PROJECT_ID", "DEX_PROJECT_SCOPE", "DEX_PROJECT_STORAGE_BUCKET",
                "DEX_PROJECT_STORAGE_KMS_KEY_ARN", "DEX_PROJECT_STORAGE_PREFIX")), fieldNames(environment.get("hosted")));
        assertEquals(new HashSet<>(Arrays.asList("DEX_PROJECT_ALLOW_LOCAL_STORAGE", "DEX_PROJECT_STORAGE_ENDPOINT")),
                fieldNames(environment.get("localObjectStoreOverrides")));
        assertEquals(ProjectConfigurationLoader.LOCAL_CONNECTIONS_FILE_ENVIRONMENT,
                fieldNames(environment.get("localDexWeb")).iterator().next());
        final Map<String, String> hosted = strings(environment.get("hosted"));
        final String kmsKey = hosted.get("DEX_PROJECT_STORAGE_KMS_KEY_ARN");
        final List<String> readKeys = new ArrayList<>();
        final S3ObjectClient fixtureS3 = new S3ObjectClient() {
            @Override
            public String getBucketVersioning(final String bucket) {
                return "Enabled";
            }

            @Override
            public GetResponse getObject(final String bucket, final String key, final String versionId,
                    final int maximumBytes) {
                readKeys.add(bucket + ":" + key + "@" + versionId);
                return new GetResponse(storage.get("snapshot").get("version").textValue(), "\"fixture-etag\"",
                        "aws:kms", kmsKey,
                        storage.get("configurationSnapshotCanonicalJSON").textValue().getBytes(StandardCharsets.UTF_8));
            }

            @Override
            public PutResponse putObject(final PutRequest request) {
                throw new AssertionError("the loader must not write");
            }
        };
        final LoadedProject project = ProjectConfigurationLoader.loadFromEnvironment(hosted, fixtureS3);
        assertJsonValueEquals(storage.get("configurationSnapshotDocument"), project.getConfiguration().toJson());
        assertEquals("example-bucket:fixtures/projects/sample-project/preview/configuration/head@1", readKeys.get(0));
        final Map<String, String> mixed = new HashMap<>(hosted);
        mixed.putAll(strings(environment.get("localDexWeb")));
        assertThrows(UnsupportedOperationException.class,
                () -> ProjectConfigurationLoader.loadFromEnvironment(mixed, fixtureS3));
        final Map<String, String> local = new HashMap<>(hosted);
        local.putAll(strings(environment.get("localObjectStoreOverrides")));
        assertEquals(project.getConfiguration().getScope(),
                ProjectConfigurationLoader.loadFromEnvironment(local, fixtureS3).getConfiguration().getScope());
        final Map<String, String> publicEndpoint = new HashMap<>(hosted);
        publicEndpoint.put("DEX_PROJECT_ALLOW_LOCAL_STORAGE", "true");
        publicEndpoint.put("DEX_PROJECT_STORAGE_ENDPOINT", "https://public.example");
        assertThrows(IllegalArgumentException.class,
                () -> ProjectConfigurationLoader.loadFromEnvironment(publicEndpoint, fixtureS3));
    }

    @Test
    void usesExactS3PreconditionsAndClassifiesFailures() throws Exception {
        final List<S3ObjectClient.PutRequest> puts = new ArrayList<>();
        final String[] versioning = {"Suspended"};
        final int[] failStatus = {0};
        final S3ObjectClient fake = new S3ObjectClient() {
            @Override
            public String getBucketVersioning(final String bucket) {
                return versioning[0];
            }

            @Override
            public GetResponse getObject(final String bucket, final String key, final String versionId,
                    final int maximumBytes) {
                return new GetResponse("v1", "\"etag\"", "AES256", "", "{}".getBytes(StandardCharsets.UTF_8));
            }

            @Override
            public PutResponse putObject(final PutRequest request) throws IOException {
                puts.add(request);
                if (failStatus[0] != 0) {
                    throw new S3RequestException(failStatus[0]);
                }
                return failStatus[0] == 0 && puts.size() > 2 ? new PutResponse("", "")
                        : new PutResponse("v1", "\"etag\"");
            }
        };
        final String kmsKey = "arn:aws:kms:us-west-2:123456789012:key/example-key";
        assertThrows(IllegalArgumentException.class, () -> S3ObjectStore.create(fake, "bucket", "", "", true));
        versioning[0] = "Enabled";
        assertThrows(IllegalArgumentException.class, () -> S3ObjectStore.create(fake, "bucket", "", "", false));
        final S3ObjectStore store = S3ObjectStore.create(fake, "bucket", "", kmsKey, false);
        store.createObject("projects/p1/live/configuration/head", "{}".getBytes(StandardCharsets.UTF_8));
        assertEquals("*", puts.get(0).getIfNoneMatch());
        assertEquals(kmsKey, puts.get(0).getKmsKeyId());
        store.compareAndSwapObject("projects/p1/live/configuration/head", "\"etag\"",
                "{\"x\":1}".getBytes(StandardCharsets.UTF_8));
        assertEquals("\"etag\"", puts.get(1).getIfMatch());
        assertThrows(OutcomeUnknownException.class,
                () -> store.createObject("projects/p1/live/head", "{}".getBytes(StandardCharsets.UTF_8)));
        failStatus[0] = 412;
        assertThrows(ObjectConflictException.class,
                () -> store.createObject("projects/p1/live/head", "{}".getBytes(StandardCharsets.UTF_8)));
        failStatus[0] = 500;
        assertThrows(OutcomeUnknownException.class,
                () -> store.createObject("projects/p1/live/head", "{}".getBytes(StandardCharsets.UTF_8)));
        assertThrows(IllegalArgumentException.class, () -> store.readObject("projects/p1/live/head", "v1"));
    }

    @Test
    void validatesObjectKeysAndBrokerEndpoints() {
        for (String key : new String[] {"", "/a", "a//b", "a/../b", "a\\b", "a\nb"}) {
            assertThrows(IllegalArgumentException.class, () -> InMemoryObjectStore.validateObjectKey(key));
        }
        ProjectConfigurationRules.validateBrokerEndpoint("http://localhost:8080/read");
        ProjectConfigurationRules.validateBrokerEndpoint("http://broker.svc.cluster.local/read");
        ProjectConfigurationRules.validateBrokerEndpoint("http://[fd00::1]/read");
        assertThrows(IllegalArgumentException.class,
                () -> ProjectConfigurationRules.validateBrokerEndpoint("http://8.8.8.8/read"));
        assertThrows(IllegalArgumentException.class,
                () -> ProjectConfigurationRules.validateBrokerEndpoint("https://user@broker.example/read"));
        assertTrue(ProjectConfigurationRules.isLocalStorageEndpoint("http://127.0.0.1:9000"));
        assertFalse(ProjectConfigurationRules.isLocalStorageEndpoint("http://127.0.0.1:9000/bucket"));
        assertEquals(Duration.ofHours(8).getSeconds(), ZoneOffset.ofHours(8).getTotalSeconds());
    }

    static ObjectNode token(final String value) {
        final ObjectNode result = GoCompatibleJson.newObject();
        result.put("token", value);
        return result;
    }

    static ObjectNode apiKey(final String value) {
        final ObjectNode result = GoCompatibleJson.newObject();
        result.put("apiKey", value);
        return result;
    }

    static void ownerOnly(final Path path) throws IOException {
        Files.setPosixFilePermissions(path, PosixFilePermissions.fromString("rw-------"));
    }

    static String repeat(final char character, final int count) {
        final char[] characters = new char[count];
        Arrays.fill(characters, character);
        return new String(characters);
    }

    private static String text(final byte[] contents) {
        return new String(contents, StandardCharsets.UTF_8);
    }

    private static Set<String> fieldNames(final JsonNode object) {
        final Set<String> names = new HashSet<>();
        final Iterator<String> iterator = object.fieldNames();
        while (iterator.hasNext()) {
            names.add(iterator.next());
        }
        return names;
    }

    private static Map<String, String> strings(final JsonNode object) {
        final Map<String, String> result = new HashMap<>();
        final Iterator<Map.Entry<String, JsonNode>> fields = object.fields();
        while (fields.hasNext()) {
            final Map.Entry<String, JsonNode> field = fields.next();
            result.put(field.getKey(), field.getValue().textValue());
        }
        return result;
    }
}
