# Dex Connectors Java SDK

Provider-neutral Java primitives for Dex-native connectors, published to Maven
Central as `io.superdurable:dex-connectors-sdk`. It is the Java counterpart of
the Go (`sdkgo`), Python (`sdkpython`), and TypeScript (`sdkts`) connector SDKs
and passes the same cross-language fixtures in [`conformance/`](../conformance).

```xml
<dependency>
  <groupId>io.superdurable</groupId>
  <artifactId>dex-connectors-sdk</artifactId>
  <version>0.1.0</version>
</dependency>
```

```groovy
implementation 'io.superdurable:dex-connectors-sdk:0.1.0'
```

Requirements:

- Java 8 or newer at run time; classes are compiled with `--release 8`, like the
  Dex Java SDK.
- Dex Java SDK `io.superdurable:dex-sdk` 1.45.2 or newer. The SDK declares
  1.45.2 as a minimum; a newer release that the application requires wins.

Applications normally use a connector package such as
`io.superdurable:dex-connector-google-gmail`, whose generated operation Steps
are built on this SDK. Use this SDK directly to write a connector or a
hand-written connector Step.

## Packages

| Package | Contents |
| --- | --- |
| `io.superdurable.dexconnectors.sdk` | Identities (`CallId`, `ConnectionRef`, `OperationRef`), definitions, `Query`/`Mutation`, attempts and results, `QueryStep`/`MutationStep`, credentials, progress |
| `...sdk.singledispatch` | `Fence`, `DispatchDecision`, `Marker`: bounded single dispatch of non-idempotent writes through Dex heartbeats |
| `...sdk.connectormock` | Scripted operation mocks and the call report |
| `...sdk.projectconfig` | Dex Web and project connection storage, credential broker, project credential provider |
| `...sdk.oauthtoken`, `...sdk.providerhttp` | Manifest-driven OAuth refresh and provider HTTP |
| `...sdk.trigger`, `...sdk.webhooktrigger`, `...sdk.relaysignature` | Trigger definitions, delivery to Dex Flows, webhook endpoints, relay signatures |
| `...sdk.projectconfig` Trigger inbox | `TriggerInbox`, `DurableTriggerTarget`, `TriggerEventCodec`: acknowledged events kept in project storage, byte-compatible with Go |
| `...sdk.pollingtrigger` | `TriggerCredentialCalls`: the stable credential Call of a polling Trigger |

## Scope

This release covers what the Gmail and Slack Java ports need. It does not yet
include the polling Trigger kit beyond `TriggerCredentialCalls`, or text
generation. Three Dex Java SDK gaps limit parity with the other SDKs; the SDK
does not work around them:

- RPC request IDs ([superdurable/dex#1864](https://github.com/superdurable/dex/issues/1864)):
  the Dex Java Client cannot send one request ID per RPC Trigger event, so an
  RPC Trigger target is not public yet. Flow-start Trigger targets are.
- Stable handler error types ([superdurable/dex#2174](https://github.com/superdurable/dex/issues/2174)):
  a Java Worker reports a handler failure by its Java class name, so other
  languages' Trigger targets cannot recognize a Java undeliverable marker.
- Step attempt deadline ([superdurable/dex#2175](https://github.com/superdurable/dex/issues/2175)):
  the Dex Java `Context` has no deadline, so the single-dispatch Fence
  approximates one: the Execute timeout minus the time since the SDK first saw
  the attempt (`ConnectorSteps.attemptObservedAt`). Time Dex spent before the
  handler started is not counted.

Local Dex Web mode (`ProjectConfigurationLoader.loadDexWebProject`) has two
deployment limits:

- One language per workspace on Linux. Java locks `connections.json.lock`
  with `fcntl`, while Go uses `flock`; on Linux the two do not exclude each
  other, so a Java process and a Go, Rust, or Python process must not refresh
  credentials from the same `connections.json`. On macOS they exclude each
  other.
- Local state directories are for one application. The local Trigger inbox
  and poll cursor files are read and compare-and-swapped under a directory
  lock, so several Java stores on one directory do not lose events, but Go
  limits its local store to one process and the Linux lock limit above
  applies to mixed languages. A refresh driver that ignores interruption after
  its timeout keeps the refresh lock for at most ten more seconds.

Connector ports such as Gmail and Slack inherit these limits: their local mode
is single-language per workspace.

## Operations

A connector implements `Query<I, O>` for a read and `Mutation<I, O>` for a write.
Each attempt classifies one provider call:

- `QueryAttempt.branch` / `MutationAttempt.branch` select a declared branch for
  a definite business outcome. Only the happy path is required; every other
  branch is `optional`.
- `retry` asks Dex to retry under the Step retry policy. A provider delay of at
  least one second becomes a Dex `RetryAfterException` truncated to whole
  seconds; a shorter delay uses the policy backoff. Exhausted retries fail the
  Flow and are never persisted as a result.
- `MutationAttempt.uncertain` is only for a possibly accepted write that
  read-back cannot confirm.

`ConnectorOperations.runQuery` and `runMutation` validate the attempt, fill in
the receipt (Call ID, provider, idempotency key, UTC observation time), and turn
invalid output into the `defect` branch.

```java
final class GetItem implements Query<String, String> {
    private static final OperationRef OPERATION = new OperationRef("sample", "getItem");

    @Override
    public QueryDefinition definition() {
        return new QueryDefinition(OPERATION, Arrays.asList(
                new BranchDefinition("found", "The item exists."),
                new BranchDefinition("missing", "The item does not exist.", true),
                new BranchDefinition("defect", "Connector defect.", true)),
                new StepDefaults(Duration.ofSeconds(15), 2));
    }

    @Override
    public QueryAttempt<String> invoke(Call call, String itemId) {
        // Call the provider here; never put credentials in the value or receipt.
        return QueryAttempt.branch("found", "item " + itemId, Receipt.empty());
    }
}
```

## Steps in a Flow

The Dex Java SDK registers Steps by class and routes with
`StepDecision.goTo(Class, input)`. Each use of a connector operation is
therefore its own class: declare it as an anonymous subclass of `QueryStep` or
`MutationStep` and keep the instance as a final field of the Flow. A branch
target receives a concrete result class, such as
`ItemResult extends QueryResult<String>`, because a Dex Step input type must be
a concrete class. Connector packages generate these result classes and typed
Step bases for every operation.

```java
public final class ItemResult extends QueryResult<String> {
    @JsonCreator
    public ItemResult(@JsonProperty(value = "branch", required = true) String branch,
                      @JsonProperty("value") String value,
                      @JsonProperty(value = "receipt", required = true) Receipt receipt,
                      @JsonProperty("failure") Failure failure) {
        super(branch, value, receipt, failure);
    }
}

public class ItemLookupFlow implements Flow<String> {
    public final Attribute<ItemResult> lastLookup = Attribute.define("lastLookup", ItemResult.class);
    private final StartLookup startLookup = new StartLookup(this);
    private final ReportItem reportItem = new ReportItem();
    final QueryStep<String, String, String, ItemResult> readItem;

    public ItemLookupFlow(GetItem getItem) {
        readItem = new QueryStep<String, String, String, ItemResult>(
                QueryStep.<String, String, String, ItemResult>newConfig(
                        "readItem", String.class,
                        new StepAnnotations("sample", "Sample", "Reads the requested item."),
                        getItem, new ConnectionRef("sample", "main"),
                        itemId -> itemId, ItemResult::new)
                        // Only the happy path is wired; an unwired optional branch fails the Flow.
                        .branch("found", ReportItem.class)
                        .resultAttribute(lastLookup)
                        .build()) { };
    }

    @Override
    public StepList<String> getSteps() {
        return StepList.startStep(startLookup).otherSteps(readItem, reportItem);
    }

    @Override
    public PersistenceSchema getPersistenceSchema() {
        return PersistenceSchema.of(lastLookup);
    }

    public static final class StartLookup implements Step<String> {
        private final ItemLookupFlow flow;

        StartLookup(ItemLookupFlow flow) {
            this.flow = flow;
        }

        @Override
        public Class<String> getInputType() {
            return String.class;
        }

        @Override
        public StepDecision execute(Context context, String itemId) {
            return StepDecision.goTo(flow.readItem.stepClass(), itemId);
        }
    }

    public static final class ReportItem implements Step<ItemResult> {
        @Override
        public Class<ItemResult> getInputType() {
            return ItemResult.class;
        }

        @Override
        public StepDecision execute(Context context, ItemResult result) {
            return StepDecision.forceComplete(result.getValue());
        }
    }
}
```

The README snippets are excerpts of the compiled
[`ItemLookupExample`](src/test/java/io/superdurable/dexconnectors/sdk/examples/ItemLookupExample.java),
where the classes are nested. Register the Flow, run a Worker, and start it with
a Client exactly as in the [Dex Java guide](https://docs.superdurable.io/reference/java/).
[`ConnectorStepsDexIntegrationTest`](src/test/java/io/superdurable/dexconnectors/sdk/integration/ConnectorStepsDexIntegrationTest.java)
runs the same shape in real Dex Flows, including a Mutation used by two Steps of
one Flow.

Step options come from the operation's `StepDefaults`; the default Execute retry
policy is one-second initial delay, coefficient 2, five-minute maximum interval,
unlimited attempts, and one-hour total duration. Override Execute options per
Step with `StepOptionsOverride`, and the send cap of a `limited` Mutation with
`MutationStep.ConfigBuilder.maxSendsOverride` (1 to 10).

## Single dispatch

A Mutation whose `execution.repeatOnUnknown` is `limited` or `unsafe` fences its
provider write so an unknown outcome is never resent beyond the cap:

```java
@Override
public MutationAttempt<String> invoke(final Call call, final String item) throws InterruptedException {
    final Fence fence = new Fence(call, Duration.ofSeconds(15), Fence.DEFAULT_WRITE_WINDOW_FRACTION,
            call.getMaxSends());
    final DispatchDecision decision = fence.decide();
    switch (decision.getKind()) {
        case SEND_ALLOWED:
        case READ_BACK_THEN_RESEND_ALLOWED: {
            // After READ_BACK_THEN_RESEND_ALLOWED, read back first when the provider supports it.
            final String id;
            try {
                id = fence.dispatch(writeDeadline -> client.create(item, call.getIdempotencyKey(), writeDeadline));
            } catch (IOException error) {
                return MutationAttempt.retry(new Failure(FailureKind.TRANSPORT, "sample", "createItem",
                        "item write failed"));
            }
            fence.recordProviderId(id);
            return MutationAttempt.branch("created", id, Receipt.newBuilder().providerObjectId(id).build());
        }
        default:
            // Read back here; select uncertain only when the write cannot be confirmed.
            return MutationAttempt.uncertain(null, new Failure(FailureKind.TRANSPORT, "sample", "createItem",
                    "item write outcome cannot be confirmed"), Receipt.empty());
    }
}
```

The send runs on its own thread so the Fence keeps heartbeating from the Step
handler thread; the send must not use the Dex Context. The handler thread
polls Dex cancellation and the write deadline; either one cancels the send's
OkHttp call before any application abort action runs, and `ProviderHttpClient`
checks the dispatch again right before the first request byte. A send whose
callback does not return keeps its Call ID occupied in this process, so other
attempts read back instead of sending, until the callback ends.

## Provider HTTP

`ProviderHttpClient` sends through OkHttp 4 (`com.squareup.okhttp3:okhttp`,
which brings the Kotlin standard library). It never follows redirects, caches,
or retries; validates the method, header names, and header values before any
lookup; and bounds each call by an absolute call timeout. A restricted
`AddressPolicy` resolves the host once, checks every answer, dials only those
addresses, and never uses an HTTP or SOCKS proxy. `AddressPolicy.UNRESTRICTED`
honors the system `ProxySelector` on every path, including single-dispatch
sends. TLS uses `HttpsURLConnection.getDefaultSSLSocketFactory()`, so trust
material an application installs there applies, with strict hostname
verification. Run the Fences of one Step sequentially on the handler
thread.

## Development

```sh
cd sdkjava
./gradlew test javadoc          # unit and conformance fixture tests, Javadoc and checkstyle
dexcli dev -dex-port 19881 -web-port 19882 -open=false   # in another terminal
./gradlew dexDevTest            # real Dex Flow tests; stop dexcli dev afterwards
```

`./gradlew validatePublication -PreleaseVersion=0.1.0` builds the Maven publication into
`build/publication-repository` and checks its artifacts. Releases are tagged
`sdkjava/vX.Y.Z` and published to Maven Central.
