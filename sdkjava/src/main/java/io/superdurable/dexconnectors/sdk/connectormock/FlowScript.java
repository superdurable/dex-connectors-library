// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.connectormock;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedList;
import java.util.List;
import java.util.function.Predicate;

/**
 * Cases scoped to one Flow ID prefix, consulted before an operation's own script.
 *
 * <p>The longest matching prefix wins. Methods are thread-safe through the owning
 * {@link ScriptedOperation} lock.
 *
 * @param <I> the operation input type
 * @param <O> the operation output type
 */
public final class FlowScript<I, O> {
    private final String prefix;
    private final Object lock;
    private final Script<I, O> script;

    FlowScript(final String prefix, final Object lock, final ScriptedOperation<I, O> owner) {
        if (prefix == null || prefix.trim().isEmpty()) {
            throw new IllegalArgumentException("Flow script prefix is required");
        }
        this.prefix = prefix;
        this.lock = lock;
        this.script = new Script<I, O>(owner);
    }

    /**
     * Returns the Flow ID prefix.
     *
     * @return the prefix
     */
    public String getPrefix() {
        return prefix;
    }

    /**
     * Queues cases that answer the next matching calls.
     *
     * @param cases the cases in answer order
     * @return this script
     */
    @SafeVarargs
    public final FlowScript<I, O> respond(final MockCase<O>... cases) {
        final List<MockCase<O>> copied = new ArrayList<>();
        for (MockCase<O> response : cases) {
            copied.add(response);
        }
        script.owner.validateCases(copied);
        synchronized (lock) {
            script.queue.addAll(copied);
        }
        return this;
    }

    /**
     * Answers every matching input with one reusable case.
     *
     * @param predicate the input predicate
     * @param response the case
     * @return this script
     */
    public FlowScript<I, O> when(final Predicate<I> predicate, final MockCase<O> response) {
        script.owner.validateCases(Arrays.asList(response));
        synchronized (lock) {
            script.rules.add(new Rule<I, O>(predicate, response));
        }
        return this;
    }

    /**
     * Sets fallback cases, repeating the last after exhaustion.
     *
     * @param cases the fallback cases
     * @return this script
     */
    @SafeVarargs
    public final FlowScript<I, O> defaults(final MockCase<O>... cases) {
        final List<MockCase<O>> copied = new ArrayList<>();
        for (MockCase<O> response : cases) {
            copied.add(response);
        }
        script.owner.validateCases(copied);
        synchronized (lock) {
            script.replaceDefaults(copied);
        }
        return this;
    }

    Script<I, O> script() {
        return script;
    }

    static final class Rule<I, O> {
        private final Predicate<I> predicate;
        private final MockCase<O> response;

        Rule(final Predicate<I> predicate, final MockCase<O> response) {
            this.predicate = predicate;
            this.response = response;
        }
    }

    static final class Script<I, O> {
        private final ScriptedOperation<I, O> owner;
        private final List<Rule<I, O>> rules = new ArrayList<>();
        private final LinkedList<MockCase<O>> queue = new LinkedList<>();
        private List<MockCase<O>> defaults = new ArrayList<>();
        private int defaultCount;

        Script(final ScriptedOperation<I, O> owner) {
            this.owner = owner;
        }

        void addRule(final Predicate<I> predicate, final MockCase<O> response) {
            rules.add(new Rule<I, O>(predicate, response));
        }

        void enqueue(final List<MockCase<O>> cases) {
            queue.addAll(cases);
        }

        void replaceDefaults(final List<MockCase<O>> cases) {
            defaults = new ArrayList<>(cases);
            defaultCount = 0;
        }

        MockCase<O> answer(final I input) {
            for (Rule<I, O> rule : rules) {
                if (rule.predicate.test(input)) {
                    return rule.response;
                }
            }
            return queue.isEmpty() ? null : queue.removeFirst();
        }

        MockCase<O> nextDefault() {
            if (defaults.isEmpty()) {
                return null;
            }
            final MockCase<O> result = defaults.get(Math.min(defaultCount, defaults.size() - 1));
            defaultCount++;
            return result;
        }
    }
}
