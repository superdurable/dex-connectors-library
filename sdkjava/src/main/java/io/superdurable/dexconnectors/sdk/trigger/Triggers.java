// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Builds Trigger runners from a source and a target.
 */
public final class Triggers {
    private Triggers() {
    }

    /**
     * Validates static binding metadata and returns it.
     *
     * @param definition the binding definition
     * @return the same definition
     * @throws IllegalArgumentException on invalid wiring
     */
    public static TriggerBindingDefinition mustTriggerBindingDefinition(final TriggerBindingDefinition definition) {
        definition.validate();
        return definition;
    }

    /**
     * Validates and constructs one Trigger runner.
     *
     * <p>{@link TriggerRunner#run} replays a {@link TriggerDeliveryReplayer} target first, then runs
     * the source with a target that consumes {@link UndeliverableTriggerException} results, logging
     * each with the binding identity, and forwards {@link TriggerDeliveryPreparer#prepareTrigger}. A
     * replay failure, an abort included, is thrown as {@code IllegalStateException("replay Trigger
     * deliveries: ...")} caused by the original; {@link InterruptedException} propagates unchanged.
     *
     * @param config the source, target, and identities
     * @param <P> the payload type
     * @return the runner
     * @throws IllegalArgumentException when the binding does not match the definition or the
     *     source or target is missing
     */
    public static <P> TriggerRunner newTrigger(final TriggerConfig<P> config) {
        if (config == null || config.getDefinition() == null || config.getBinding() == null) {
            throw new IllegalArgumentException("trigger definition and binding are required");
        }
        config.getDefinition().validate();
        config.getBinding().validate();
        if (!config.getBinding().getTrigger().equals(config.getDefinition().getTrigger())) {
            throw new IllegalArgumentException("trigger binding does not match its definition");
        }
        if (config.getSource() == null || config.getTarget() == null) {
            throw new IllegalArgumentException("trigger source and target are required");
        }
        final TriggerBindingRef binding = config.getBinding();
        final Map<String, Object> bindingAttributes = new LinkedHashMap<>();
        bindingAttributes.put("connector", binding.getTrigger().getConnectorId());
        bindingAttributes.put("connection", binding.getConnection().getName());
        bindingAttributes.put("trigger", binding.getTrigger().getTriggerName());
        bindingAttributes.put("binding", binding.getName());
        return new ConfiguredTriggerRunner<P>(config,
                TriggerDelivery.bindTriggerLogAttributes(config.getLogger(), bindingAttributes));
    }

    private static final class ConfiguredTriggerRunner<P> implements TriggerRunner {
        private final TriggerConfig<P> config;
        private final TriggerLogger logger;

        private ConfiguredTriggerRunner(final TriggerConfig<P> config, final TriggerLogger logger) {
            this.config = config;
            this.logger = logger;
        }

        @Override
        public TriggerDefinition getDefinition() {
            return config.getDefinition();
        }

        @Override
        public TriggerBindingRef getBinding() {
            return config.getBinding();
        }

        @Override
        public void run(final TriggerContext context) throws Exception {
            final TriggerContext deliveryContext = context == null ? new TriggerContext() : context;
            final TriggerTarget<P> target = config.getTarget();
            if (target instanceof TriggerDeliveryReplayer) {
                try {
                    ((TriggerDeliveryReplayer) target).replayTriggerDeliveries(deliveryContext);
                } catch (InterruptedException error) {
                    throw error;
                } catch (Exception error) {
                    throw new IllegalStateException("replay Trigger deliveries: " + error.getMessage(), error);
                }
            }
            config.getSource().run(deliveryContext, new UndeliverableConsumingTarget<P>(target, logger));
        }
    }

    private static final class UndeliverableConsumingTarget<P> implements TriggerTarget<P>, TriggerDeliveryPreparer<P> {
        private final TriggerTarget<P> target;
        private final TriggerLogger logger;

        private UndeliverableConsumingTarget(final TriggerTarget<P> target, final TriggerLogger logger) {
            this.target = target;
            this.logger = logger;
        }

        @Override
        public void handleTrigger(final TriggerContext context, final TriggerEvent<P> event) throws Exception {
            try {
                target.handleTrigger(context, event);
            } catch (InterruptedException error) {
                throw error;
            } catch (Exception error) {
                if (!TriggerDelivery.isTriggerUndeliverable(error)) {
                    throw error;
                }
                final Map<String, Object> attributes = TriggerDelivery.eventAttributes(event);
                attributes.putAll(TriggerDelivery.triggerErrorAttributes(error));
                TriggerDelivery.logTriggerRecord(logger, context, TriggerLogLevel.WARN,
                        "trigger event skipped: undeliverable", attributes);
                context.recordSkip();
            }
        }

        @Override
        public void prepareTrigger(final TriggerContext context, final TriggerEvent<P> event) throws Exception {
            TriggerDelivery.prepareTriggerDelivery(context, target, event);
        }
    }
}
