// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.connectormock;

import java.io.IOException;
import java.io.RandomAccessFile;
import java.nio.charset.StandardCharsets;
import java.nio.file.FileAlreadyExistsException;
import java.nio.file.Files;
import java.nio.file.NoSuchFileException;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.Map;

/**
 * Appends answered mock calls to the shared connector call report.
 *
 * <p>When the {@value #REPORT_DIRECTORY_ENVIRONMENT} environment variable names a directory, every
 * answered call appends one row to {@value #REPORT_FILE_NAME} there as Go-compatible compact JSON.
 * Appends from several processes are serialized with a sibling lock file.
 */
public final class ConnectorMockReport {
    /** Environment variable that names the report directory. */
    public static final String REPORT_DIRECTORY_ENVIRONMENT = "DEX_CONNECTOR_MOCK_REPORT_DIR";
    /** File name of the report inside the report directory. */
    public static final String REPORT_FILE_NAME = "connector-calls.json";

    private static final Object PROCESS_LOCK = new Object();
    private static final long LOCK_WAIT_MILLIS = 30_000;
    private static final long STALE_LOCK_MILLIS = 10_000;

    private ConnectorMockReport() {
    }

    /**
     * Appends one row to the report named by the environment, or does nothing when it is unset.
     *
     * @param call the answered call
     * @throws IOException if the report cannot be written or is malformed
     */
    public static void appendToReport(final RecordedCall<?> call) throws IOException {
        final String directory = System.getenv(REPORT_DIRECTORY_ENVIRONMENT);
        if (directory == null || directory.isEmpty()) {
            return;
        }
        appendToReport(call, Paths.get(directory));
    }

    /**
     * Appends one row to the report in the given directory.
     *
     * @param call the answered call
     * @param directory the report directory
     * @throws IOException if the report cannot be written or is malformed
     */
    public static void appendToReport(final RecordedCall<?> call, final Path directory) throws IOException {
        final Path reportPath = directory.resolve(REPORT_FILE_NAME);
        final Path lockPath = directory.resolve(REPORT_FILE_NAME + ".lock");
        final byte[] encoded = encodeGoJson(call.toReportRow()).getBytes(StandardCharsets.UTF_8);
        synchronized (PROCESS_LOCK) {
            acquireLock(lockPath);
            try (RandomAccessFile destination = new RandomAccessFile(reportPath.toFile(), "rw")) {
                final long size = destination.length();
                final byte[] entry;
                if (size == 0) {
                    entry = concat("{\"calls\":[".getBytes(StandardCharsets.UTF_8), encoded,
                            "]}".getBytes(StandardCharsets.UTF_8));
                    destination.seek(0);
                } else {
                    if (size < 2) {
                        throw new IOException("connector call report has no closing bracket");
                    }
                    destination.seek(size - 2);
                    if (destination.read() != ']' || destination.read() != '}') {
                        throw new IOException("connector call report has no closing bracket");
                    }
                    entry = concat(",".getBytes(StandardCharsets.UTF_8), encoded,
                            "]}".getBytes(StandardCharsets.UTF_8));
                    destination.seek(size - 2);
                }
                destination.write(entry);
                destination.setLength(destination.getFilePointer());
            } finally {
                Files.deleteIfExists(lockPath);
            }
        }
    }

    private static void acquireLock(final Path lockPath) throws IOException {
        final long deadline = System.currentTimeMillis() + LOCK_WAIT_MILLIS;
        while (true) {
            try {
                Files.createFile(lockPath);
                return;
            } catch (FileAlreadyExistsException held) {
                try {
                    if (System.currentTimeMillis() - Files.getLastModifiedTime(lockPath).toMillis() > STALE_LOCK_MILLIS) {
                        Files.deleteIfExists(lockPath);
                        continue;
                    }
                } catch (NoSuchFileException released) {
                    continue;
                }
                if (System.currentTimeMillis() >= deadline) {
                    throw new IOException("connector call report lock stayed held");
                }
                try {
                    Thread.sleep(5);
                } catch (InterruptedException error) {
                    Thread.currentThread().interrupt();
                    throw new IOException("interrupted while waiting for the connector call report lock", error);
                }
            }
        }
    }

    // Go's encoding/json escapes HTML-sensitive characters and line separators.
    static String encodeGoJson(final Map<String, Object> row) {
        final StringBuilder builder = new StringBuilder("{");
        boolean isFirst = true;
        for (Map.Entry<String, Object> field : row.entrySet()) {
            if (!isFirst) {
                builder.append(',');
            }
            isFirst = false;
            appendGoString(builder, field.getKey());
            builder.append(':');
            if (field.getValue() instanceof Boolean) {
                builder.append(field.getValue().toString());
            } else {
                appendGoString(builder, String.valueOf(field.getValue()));
            }
        }
        return builder.append('}').toString();
    }

    private static void appendGoString(final StringBuilder builder, final String value) {
        builder.append('"');
        for (int index = 0; index < value.length(); index++) {
            final char character = value.charAt(index);
            switch (character) {
                case '"':
                    builder.append("\\\"");
                    break;
                case '\\':
                    builder.append("\\\\");
                    break;
                case '\n':
                    builder.append("\\n");
                    break;
                case '\r':
                    builder.append("\\r");
                    break;
                case '\t':
                    builder.append("\\t");
                    break;
                case '<':
                case '>':
                case '&':
                case ' ':
                case ' ':
                    builder.append(String.format("\\u%04x", (int) character));
                    break;
                default:
                    if (character < 0x20) {
                        builder.append(String.format("\\u%04x", (int) character));
                    } else {
                        builder.append(character);
                    }
            }
        }
        builder.append('"');
    }

    private static byte[] concat(final byte[]... parts) {
        int length = 0;
        for (byte[] part : parts) {
            length += part.length;
        }
        final byte[] result = new byte[length];
        int offset = 0;
        for (byte[] part : parts) {
            System.arraycopy(part, 0, result, offset, part.length);
            offset += part.length;
        }
        return result;
    }
}
