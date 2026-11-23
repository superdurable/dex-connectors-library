// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import io.superdurable.dex.Attribute;
import io.superdurable.dex.BufferedTextStream;
import io.superdurable.dex.Context;
import io.superdurable.dex.Step;
import io.superdurable.dex.StepDecision;
import io.superdurable.dex.StepOptions;
import io.superdurable.dex.Stream;
import io.superdurable.dexconnectors.sdk.singledispatch.SendCaps;
import java.time.Duration;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;
import java.util.Set;
import java.util.function.Function;

/**
 * Invokes one Mutation per Dex Execute attempt and routes its terminal branch.
 *
 * <p>The Dex Java SDK registers Steps by class, so each use of a Mutation in a Flow is its own
 * subclass; declare it as an anonymous subclass at the use site, as shown in {@link QueryStep}.
 * The operation supplies a stable idempotency key for retries, and only declared terminal branches
 * may route to another Step.
 *
 * <p>{@link ConfigBuilder#maxSendsOverride} replaces the send cap of a {@code limited} operation
 * for this Step, within 1..10; 1 disables resend. Operations that are {@code safe},
 * {@code unsafe}, or undeclared reject it. When the operation's retry policy limits attempts, the
 * Step adds attempts for a raised cap so it stays reachable; an explicit
 * {@link StepOptionsOverride.Builder#executeRetry} still wins.
 *
 * @param <I> the Step input type
 * @param <OI> the operation input type
 * @param <O> the operation output type
 * @param <R> the concrete Mutation result class routed to branch targets
 */
public abstract class MutationStep<I, OI, O, R extends MutationResult<O>> implements Step<I> {
    private final Config<I, OI, O, R> config;
    private final OperationRef operationRef;
    private final Map<String, Class<? extends Step<R>>> branches;
    private final Set<String> optionalBranches = new HashSet<>();
    private final StepOptions options;
    private final int maxSends;

    /**
     * Validates the configuration and binds it to this Step class.
     *
     * @param config the Step configuration
     * @throws IllegalArgumentException if the definition, branch wiring, or send cap is invalid
     */
    protected MutationStep(final Config<I, OI, O, R> config) {
        Objects.requireNonNull(config, "config");
        final MutationDefinition definition = config.operation.definition();
        definition.validate();
        config.connection.validate();
        this.branches = ConnectorSteps.validateBranches(definition.getBranches(), config.branches, optionalBranches);
        this.config = config;
        this.operationRef = definition.getOperation();
        final boolean isLimited = definition.getRepeatOnUnknown() == RepeatOnUnknown.LIMITED;
        int additionalExecuteAttempts = 0;
        if (config.maxSendsOverride == null) {
            this.maxSends = isLimited ? definition.getMaxSends() : SendCaps.MINIMUM_MAX_SENDS;
        } else {
            SendCaps.validateMaxSends(config.maxSendsOverride);
            if (!isLimited) {
                throw new IllegalArgumentException("maxSendsOverride requires a repeatOnUnknown limited operation");
            }
            this.maxSends = config.maxSendsOverride;
            additionalExecuteAttempts = Math.max(0, config.maxSendsOverride - definition.getMaxSends());
        }
        this.options = StepOptionsOverride.mergeStepOptions(
                definition.getStepDefaults(), config.stepOptionsOverride, additionalExecuteAttempts);
    }

    @Override
    public final Class<I> getInputType() {
        return config.stepInputType;
    }

    @Override
    public final String getStepType() {
        return config.stepType;
    }

    @Override
    public final StepOptions getStepOptions() {
        return options;
    }

    /**
     * Returns this Step's class for {@code StepDecision.goTo} and {@code StepMovement.of}.
     *
     * @return the Step class
     */
    @SuppressWarnings("unchecked")
    public final Class<? extends Step<I>> stepClass() {
        return (Class<? extends Step<I>>) getClass();
    }

    /**
     * Returns the single-dispatch send cap every attempt of this Step receives.
     *
     * @return the send cap within 1..10
     */
    public final int getMaxSends() {
        return maxSends;
    }

    /**
     * Returns the authoring group and explanation for this Step.
     *
     * @return the annotations
     */
    public final StepAnnotations getAnnotations() {
        return config.annotations;
    }

    /**
     * Returns the connector-owned configuration units for this Step.
     *
     * @return the configuration UI
     */
    public final ConnectorConfigurationUI getConfigurationUI() {
        return config.configurationUI;
    }

    /**
     * Invokes the Mutation, persists its result if configured, and routes its branch.
     *
     * @param context the Dex Step Context
     * @param input the Step input
     * @return the branch decision
     */
    @Override
    public final StepDecision execute(final Context context, final I input) {
        final BufferedTextStream textWriter = config.textStream == null ? null
                : BufferedTextStream.create(context, config.textStream, config.textFlushInterval,
                        config.textMaxBufferedBytes);
        final Call call = Call.forStep(context, config.connection, operationRef, maxSends,
                config.progressStream == null ? null
                        : (callContext, callId) -> new Call.ProgressReporter(config.progressStream, callContext, callId),
                textWriter);
        final MutationResult<O> result;
        try {
            result = ConnectorOperations.runMutation(config.operation, call, config.mapToOperationInput.apply(input));
        } catch (RetryException error) {
            throw ConnectorSteps.throwDexRetry(error);
        } catch (InterruptedException error) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("connector Mutation was interrupted", error);
        }
        final R typedResult = config.resultFactory.create(
                result.getBranch(), result.getValue(), result.getReceipt(), result.getFailure());
        if (config.resultAttribute != null) {
            config.resultAttribute.set(context, typedResult);
        }
        return ConnectorSteps.routeBranch(branches, optionalBranches, result.getBranch(), typedResult,
                result.getFailure(), call, "mutation");
    }

    /**
     * Creates a configuration builder with the required values.
     *
     * @param stepType the stable Step type registered with Dex
     * @param stepInputType the concrete Step input class
     * @param annotations the group and explanation
     * @param operation the Mutation
     * @param connection the logical connection
     * @param mapToOperationInput maps the Step input to the operation input
     * @param resultFactory creates the concrete result class
     * @param <I> the Step input type
     * @param <OI> the operation input type
     * @param <O> the operation output type
     * @param <R> the concrete result class
     * @return a new builder
     */
    public static <I, OI, O, R extends MutationResult<O>> ConfigBuilder<I, OI, O, R> newConfig(
            final String stepType, final Class<I> stepInputType, final StepAnnotations annotations,
            final Mutation<OI, O> operation, final ConnectionRef connection,
            final Function<I, OI> mapToOperationInput, final MutationResultFactory<O, R> resultFactory) {
        return new ConfigBuilder<I, OI, O, R>(stepType, stepInputType, annotations, operation, connection,
                mapToOperationInput, resultFactory);
    }

    /**
     * Creates a concrete Mutation result from its validated parts.
     *
     * @param <O> the operation output type
     * @param <R> the concrete result class
     */
    public interface MutationResultFactory<O, R extends MutationResult<O>> {
        /**
         * Creates the concrete result.
         *
         * @param branch the terminal branch
         * @param value the operation output
         * @param receipt the completed receipt
         * @param failure the failure, or {@code null}
         * @return the concrete result
         */
        R create(String branch, O value, Receipt receipt, Failure failure);
    }

    /**
     * Immutable Mutation Step configuration.
     *
     * @param <I> the Step input type
     * @param <OI> the operation input type
     * @param <O> the operation output type
     * @param <R> the concrete result class
     */
    public static final class Config<I, OI, O, R extends MutationResult<O>> {
        private final String stepType;
        private final Class<I> stepInputType;
        private final StepAnnotations annotations;
        private final Mutation<OI, O> operation;
        private final ConnectionRef connection;
        private final Function<I, OI> mapToOperationInput;
        private final MutationResultFactory<O, R> resultFactory;
        private final Map<String, Class<? extends Step<R>>> branches;
        private final ConnectorConfigurationUI configurationUI;
        private final Attribute<R> resultAttribute;
        private final Stream<ProgressUpdate> progressStream;
        private final Stream<String> textStream;
        private final Duration textFlushInterval;
        private final int textMaxBufferedBytes;
        private final StepOptionsOverride stepOptionsOverride;
        private final Integer maxSendsOverride;

        private Config(final ConfigBuilder<I, OI, O, R> builder) {
            this.stepType = builder.stepType;
            this.stepInputType = builder.stepInputType;
            this.annotations = builder.annotations;
            this.operation = builder.operation;
            this.connection = builder.connection;
            this.mapToOperationInput = builder.mapToOperationInput;
            this.resultFactory = builder.resultFactory;
            this.branches = new LinkedHashMap<>(builder.branches);
            this.configurationUI = builder.configurationUI;
            this.resultAttribute = builder.resultAttribute;
            this.progressStream = builder.progressStream;
            this.textStream = builder.textStream;
            this.textFlushInterval = builder.textFlushInterval;
            this.textMaxBufferedBytes = builder.textMaxBufferedBytes;
            this.stepOptionsOverride = builder.stepOptionsOverride;
            this.maxSendsOverride = builder.maxSendsOverride;
        }
    }

    /**
     * Builds a Mutation Step {@link Config}.
     *
     * @param <I> the Step input type
     * @param <OI> the operation input type
     * @param <O> the operation output type
     * @param <R> the concrete result class
     */
    public static final class ConfigBuilder<I, OI, O, R extends MutationResult<O>> {
        private final String stepType;
        private final Class<I> stepInputType;
        private final StepAnnotations annotations;
        private final Mutation<OI, O> operation;
        private final ConnectionRef connection;
        private final Function<I, OI> mapToOperationInput;
        private final MutationResultFactory<O, R> resultFactory;
        private final Map<String, Class<? extends Step<R>>> branches = new LinkedHashMap<>();
        private ConnectorConfigurationUI configurationUI = ConnectorConfigurationUI.EMPTY;
        private Attribute<R> resultAttribute;
        private Stream<ProgressUpdate> progressStream;
        private Stream<String> textStream;
        private Duration textFlushInterval = BufferedTextStream.DEFAULT_FLUSH_INTERVAL;
        private int textMaxBufferedBytes = BufferedTextStream.DEFAULT_MAX_BUFFERED_BYTES;
        private StepOptionsOverride stepOptionsOverride;
        private Integer maxSendsOverride;

        private ConfigBuilder(
                final String stepType, final Class<I> stepInputType, final StepAnnotations annotations,
                final Mutation<OI, O> operation, final ConnectionRef connection,
                final Function<I, OI> mapToOperationInput, final MutationResultFactory<O, R> resultFactory) {
            if (stepType == null || stepType.trim().isEmpty()) {
                throw new IllegalArgumentException("stable Step type is required");
            }
            if (stepInputType == null) {
                throw new IllegalArgumentException("Step input type is required");
            }
            if (annotations == null || operation == null || connection == null) {
                throw new IllegalArgumentException("annotations, mutation operation, and connection are required");
            }
            if (mapToOperationInput == null || resultFactory == null) {
                throw new IllegalArgumentException("mapToOperationInput and resultFactory are required");
            }
            this.stepType = stepType;
            this.stepInputType = stepInputType;
            this.annotations = annotations;
            this.operation = operation;
            this.connection = connection;
            this.mapToOperationInput = mapToOperationInput;
            this.resultFactory = resultFactory;
        }

        /**
         * Routes a declared branch to a Step that accepts the concrete result.
         *
         * @param branchId the declared branch
         * @param target the target Step class
         * @return this builder
         */
        public ConfigBuilder<I, OI, O, R> branch(final String branchId, final Class<? extends Step<R>> target) {
            this.branches.put(branchId, target);
            return this;
        }

        /**
         * Sets the connector-owned configuration units shown for this Step.
         *
         * @param value the configuration UI
         * @return this builder
         */
        public ConfigBuilder<I, OI, O, R> configurationUI(final ConnectorConfigurationUI value) {
            value.validate();
            this.configurationUI = value;
            return this;
        }

        /**
         * Persists every terminal result in a registered Attribute.
         *
         * @param value the Attribute
         * @return this builder
         */
        public ConfigBuilder<I, OI, O, R> resultAttribute(final Attribute<R> value) {
            this.resultAttribute = value;
            return this;
        }

        /**
         * Emits structured progress to a registered Stream.
         *
         * @param value the progress Stream
         * @return this builder
         */
        public ConfigBuilder<I, OI, O, R> progressStream(final Stream<ProgressUpdate> value) {
            this.progressStream = value;
            return this;
        }

        /**
         * Emits ordered text to a registered String Stream through a buffered writer.
         *
         * @param value the text Stream
         * @param flushInterval the positive flush interval
         * @param maxBufferedBytes the positive soft buffer threshold
         * @return this builder
         */
        public ConfigBuilder<I, OI, O, R> textStream(
                final Stream<String> value, final Duration flushInterval, final int maxBufferedBytes) {
            this.textStream = value;
            this.textFlushInterval = flushInterval;
            this.textMaxBufferedBytes = maxBufferedBytes;
            return this;
        }

        /**
         * Overrides the operation's Execute defaults for this Step.
         *
         * @param value the override
         * @return this builder
         */
        public ConfigBuilder<I, OI, O, R> stepOptionsOverride(final StepOptionsOverride value) {
            this.stepOptionsOverride = value;
            return this;
        }

        /**
         * Replaces the send cap of a {@code limited} operation for this Step.
         *
         * @param value the send cap within 1..10
         * @return this builder
         */
        public ConfigBuilder<I, OI, O, R> maxSendsOverride(final int value) {
            this.maxSendsOverride = value;
            return this;
        }

        /**
         * Builds the configuration.
         *
         * @return the configuration
         */
        public Config<I, OI, O, R> build() {
            return new Config<I, OI, O, R>(this);
        }
    }
}
