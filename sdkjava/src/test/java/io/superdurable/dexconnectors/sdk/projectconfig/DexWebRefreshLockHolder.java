// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Paths;

/** Child process for tests: holds the Dex Web refresh lock until a line arrives on standard input. */
public final class DexWebRefreshLockHolder {
    private DexWebRefreshLockHolder() {
    }

    /**
     * Holds the lock of the connections file at {@code arguments[0]}.
     *
     * @param arguments the connections file path
     * @throws Exception if the lock cannot be held
     */
    public static void main(final String[] arguments) throws Exception {
        final DexWebRefreshLock.Held held =
                new DexWebRefreshLock(Paths.get(arguments[0]), DexWebRefreshLock.MAXIMUM_WAIT).acquire();
        try {
            System.out.println("locked");
            System.out.flush();
            new BufferedReader(new InputStreamReader(System.in, StandardCharsets.UTF_8)).readLine();
        } finally {
            held.close();
        }
    }
}
