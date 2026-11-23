// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import io.superdurable.dexconnectors.sdk.singledispatch.DispatchObservation;
import java.io.ByteArrayOutputStream;
import java.io.FilterInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import javax.net.ssl.SSLParameters;
import javax.net.ssl.SSLSocket;
import javax.net.ssl.SSLSocketFactory;

/**
 * One HTTP/1.1 exchange over a socket the SDK owns, so cancellation can never reopen it.
 *
 * <p>Unlike {@code HttpURLConnection}, which silently reconnects after {@code disconnect()}, this
 * exchange dials one of the given addresses once, checks a shared cancelled flag under one lock
 * immediately before connecting and before writing request bytes, and {@link #cancel()} closes the
 * TCP socket under that lock. The request carries {@code Connection: close}; the response body is
 * delimited by {@code Content-Length}, chunked encoding, or connection close.
 */
final class SocketHttpExchange {
    private static final int MAXIMUM_HEADER_BYTES = 64 * 1024;

    /** Runs after the last pre-write check and before the first request byte; tests only. */
    static volatile Runnable beforeRequestWriteForTests;

    private final Object lock = new Object();
    private boolean isCancelled;
    private Socket rawSocket;

    /** Closes the socket and forbids any later connect or write; safe from any thread. */
    void cancel() {
        final Socket socket;
        synchronized (lock) {
            isCancelled = true;
            socket = rawSocket;
        }
        closeQuietly(socket);
    }

    ProviderHttpResponse send(
            final URI uri, final List<InetAddress> addresses, final String method, final Map<String, String> headers,
            final byte[] body, final Duration timeout) throws IOException {
        final boolean isHttps = "https".equals(uri.getScheme().toLowerCase(Locale.ROOT));
        final int port = uri.getPort() > 0 ? uri.getPort() : isHttps ? 443 : 80;
        final byte[] requestHead = requestHead(uri, method, headers, body);
        final Socket socket = connect(addresses, port, timeout);
        try {
            final Socket exchangeSocket = isHttps ? startTls(socket, hostName(uri), port) : socket;
            final OutputStream output = exchangeSocket.getOutputStream();
            synchronized (lock) {
                if (isCancelled || !DispatchObservation.isActive()) {
                    throw new ProviderTransportException(true, null, null);
                }
            }
            final Runnable hook = beforeRequestWriteForTests;
            if (hook != null) {
                hook.run();
            }
            try {
                output.write(requestHead);
                if (body != null) {
                    output.write(body);
                }
                output.flush();
            } catch (IOException error) {
                throw new ProviderTransportException(false, null, null);
            }
            return readResponse(exchangeSocket, method);
        } catch (IOException | RuntimeException error) {
            closeQuietly(socket);
            throw error;
        }
    }

    private Socket connect(final List<InetAddress> addresses, final int port, final Duration timeout)
            throws ProviderTransportException {
        final Instant deadline = Instant.now().plus(timeout);
        for (InetAddress address : addresses) {
            final Socket socket = new Socket();
            synchronized (lock) {
                if (isCancelled || !DispatchObservation.isActive()) {
                    throw new ProviderTransportException(true, null, null);
                }
                rawSocket = socket;
            }
            final long remainingMillis = Duration.between(Instant.now(), deadline).toMillis();
            if (remainingMillis <= 0) {
                break;
            }
            try {
                socket.connect(new InetSocketAddress(address, port), timeoutMillis(remainingMillis));
                socket.setSoTimeout(timeoutMillis(remainingMillis));
                socket.setTcpNoDelay(true);
            } catch (IOException error) {
                closeQuietly(socket);
                continue;
            }
            synchronized (lock) {
                if (isCancelled) {
                    closeQuietly(socket);
                    throw new ProviderTransportException(true, null, null);
                }
            }
            return socket;
        }
        throw new ProviderTransportException(true, null, null);
    }

    private static Socket startTls(final Socket socket, final String host, final int port)
            throws ProviderTransportException {
        try {
            final SSLSocket tls = (SSLSocket) ((SSLSocketFactory) SSLSocketFactory.getDefault())
                    .createSocket(socket, host, port, true);
            final SSLParameters parameters = tls.getSSLParameters();
            parameters.setEndpointIdentificationAlgorithm("HTTPS");
            tls.setSSLParameters(parameters);
            tls.setUseClientMode(true);
            tls.startHandshake();
            return tls;
        } catch (IOException error) {
            throw new ProviderTransportException(true, null, null);
        }
    }

    private ProviderHttpResponse readResponse(final Socket socket, final String method) throws IOException {
        final InputStream input;
        final int statusCode;
        final Map<String, List<String>> headerValues;
        try {
            input = socket.getInputStream();
            int code;
            Map<String, List<String>> values;
            while (true) {
                final List<String> lines = readHeaderLines(input);
                code = parseStatus(lines.get(0));
                values = parseHeaders(lines);
                // Interim responses other than a protocol switch precede the final response.
                if (code < 100 || code >= 200 || code == 101) {
                    break;
                }
            }
            statusCode = code;
            headerValues = values;
        } catch (IOException error) {
            throw new ProviderTransportException(false, null, null);
        }
        DispatchObservation.recordResponseStatus(statusCode);
        final ProviderHttpHeaders headers = new ProviderHttpHeaders(headerValues);
        final InputStream body;
        if ("HEAD".equals(method) || statusCode == 204 || statusCode == 304) {
            body = null;
        } else if (isChunked(headers)) {
            body = new ChunkedBody(input);
        } else if (headers.first("Content-Length") != null) {
            final long length;
            try {
                length = Long.parseLong(headers.first("Content-Length").trim());
            } catch (NumberFormatException error) {
                throw new ProviderTransportException(false, statusCode, headers);
            }
            if (length < 0) {
                throw new ProviderTransportException(false, statusCode, headers);
            }
            body = new LengthBody(input, length);
        } else {
            body = input;
        }
        return new ProviderHttpResponse(statusCode, headers, body, () -> {
            synchronized (lock) {
                isCancelled = true;
            }
            closeQuietly(socket);
            closeQuietly(rawSocket);
        });
    }

    private static byte[] requestHead(
            final URI uri, final String method, final Map<String, String> headers, final byte[] body) {
        final StringBuilder head = new StringBuilder();
        final String path = uri.getRawPath() == null || uri.getRawPath().isEmpty() ? "/" : uri.getRawPath();
        head.append(method).append(' ').append(path);
        if (uri.getRawQuery() != null) {
            head.append('?').append(uri.getRawQuery());
        }
        head.append(" HTTP/1.1\r\n");
        final Map<String, String> lines = new LinkedHashMap<>();
        lines.put("Host", uri.getRawAuthority().contains("@")
                ? uri.getRawAuthority().substring(uri.getRawAuthority().indexOf('@') + 1) : uri.getRawAuthority());
        boolean hasUserAgent = false;
        for (Map.Entry<String, String> header : headers.entrySet()) {
            final String name = header.getKey();
            if (isHopByHop(name)) {
                continue;
            }
            hasUserAgent |= "User-Agent".equalsIgnoreCase(name);
            lines.put(name, header.getValue());
        }
        if (!hasUserAgent) {
            lines.put("User-Agent", "Java/" + System.getProperty("java.version", "8"));
        }
        if (body != null) {
            lines.put("Content-Length", Integer.toString(body.length));
        }
        lines.put("Connection", "close");
        for (Map.Entry<String, String> line : lines.entrySet()) {
            requireHeaderText(line.getKey());
            requireHeaderText(line.getValue());
            head.append(line.getKey()).append(": ").append(line.getValue()).append("\r\n");
        }
        head.append("\r\n");
        return head.toString().getBytes(StandardCharsets.ISO_8859_1);
    }

    private static boolean isHopByHop(final String name) {
        return "Host".equalsIgnoreCase(name) || "Content-Length".equalsIgnoreCase(name)
                || "Connection".equalsIgnoreCase(name) || "Transfer-Encoding".equalsIgnoreCase(name);
    }

    private static void requireHeaderText(final String value) {
        for (int index = 0; index < value.length(); index++) {
            final char character = value.charAt(index);
            if (character == '\r' || character == '\n' || character == 0 || character > 0xff) {
                throw new IllegalArgumentException("provider request header contains an invalid character");
            }
        }
    }

    private static List<String> readHeaderLines(final InputStream input) throws IOException {
        final List<String> lines = new ArrayList<>();
        final ByteArrayOutputStream line = new ByteArrayOutputStream();
        int total = 0;
        while (true) {
            final int value = input.read();
            if (value < 0) {
                throw new IOException("provider closed the connection before the response headers");
            }
            if (++total > MAXIMUM_HEADER_BYTES) {
                throw new IOException("provider response headers are too large");
            }
            if (value == '\n') {
                String text = new String(line.toByteArray(), StandardCharsets.ISO_8859_1);
                if (text.endsWith("\r")) {
                    text = text.substring(0, text.length() - 1);
                }
                line.reset();
                if (text.isEmpty()) {
                    if (lines.isEmpty()) {
                        continue;
                    }
                    return lines;
                }
                lines.add(text);
                continue;
            }
            line.write(value);
        }
    }

    private static int parseStatus(final String statusLine) throws IOException {
        final String[] parts = statusLine.split(" ", 3);
        if (parts.length < 2 || !parts[0].startsWith("HTTP/1.") || parts[1].length() != 3) {
            throw new IOException("provider response status line is invalid");
        }
        try {
            return Integer.parseInt(parts[1]);
        } catch (NumberFormatException error) {
            throw new IOException("provider response status line is invalid");
        }
    }

    private static Map<String, List<String>> parseHeaders(final List<String> lines) throws IOException {
        final Map<String, List<String>> values = new LinkedHashMap<>();
        for (String line : lines.subList(1, lines.size())) {
            final int colon = line.indexOf(':');
            if (colon <= 0) {
                throw new IOException("provider response header is invalid");
            }
            final String name = line.substring(0, colon).trim();
            final String value = line.substring(colon + 1).trim();
            List<String> existing = null;
            for (Map.Entry<String, List<String>> entry : values.entrySet()) {
                if (entry.getKey().equalsIgnoreCase(name)) {
                    existing = entry.getValue();
                    break;
                }
            }
            if (existing == null) {
                existing = new ArrayList<>();
                values.put(name, existing);
            }
            existing.add(value);
        }
        return values;
    }

    private static boolean isChunked(final ProviderHttpHeaders headers) {
        for (String value : headers.all("Transfer-Encoding")) {
            if (value.toLowerCase(Locale.ROOT).contains("chunked")) {
                return true;
            }
        }
        return false;
    }

    private static String hostName(final URI uri) {
        final String host = uri.getHost();
        return host.startsWith("[") && host.endsWith("]") ? host.substring(1, host.length() - 1) : host;
    }

    private static int timeoutMillis(final long millis) {
        return (int) Math.max(1, Math.min(Integer.MAX_VALUE, millis));
    }

    private static void closeQuietly(final Socket socket) {
        if (socket == null) {
            return;
        }
        try {
            socket.close();
        } catch (IOException ignored) {
            // Closing an already failed socket has nothing further to release.
        }
    }

    /** A response body delimited by {@code Content-Length}. */
    private static final class LengthBody extends FilterInputStream {
        private long remaining;

        private LengthBody(final InputStream input, final long length) {
            super(input);
            this.remaining = length;
        }

        @Override
        public int read() throws IOException {
            final byte[] single = new byte[1];
            final int count = read(single, 0, 1);
            return count < 0 ? -1 : single[0] & 0xff;
        }

        @Override
        public int read(final byte[] buffer, final int offset, final int length) throws IOException {
            if (remaining <= 0) {
                return -1;
            }
            final int count = super.read(buffer, offset, (int) Math.min(length, remaining));
            if (count < 0) {
                throw new IOException("provider response body ended early");
            }
            remaining -= count;
            return count;
        }
    }

    /** A response body in HTTP/1.1 chunked transfer encoding. */
    private static final class ChunkedBody extends InputStream {
        private final InputStream input;
        private long chunkRemaining;
        private boolean isFinished;

        private ChunkedBody(final InputStream input) {
            this.input = input;
        }

        @Override
        public int read() throws IOException {
            final byte[] single = new byte[1];
            final int count = read(single, 0, 1);
            return count < 0 ? -1 : single[0] & 0xff;
        }

        @Override
        public int read(final byte[] buffer, final int offset, final int length) throws IOException {
            if (isFinished) {
                return -1;
            }
            if (chunkRemaining == 0) {
                chunkRemaining = readChunkSize();
                if (chunkRemaining == 0) {
                    // Trailer fields end with an empty line.
                    while (!readLine().isEmpty()) {
                        continue;
                    }
                    isFinished = true;
                    return -1;
                }
            }
            final int count = input.read(buffer, offset, (int) Math.min(length, chunkRemaining));
            if (count < 0) {
                throw new IOException("provider response body ended early");
            }
            chunkRemaining -= count;
            if (chunkRemaining == 0) {
                readLine();
            }
            return count;
        }

        private long readChunkSize() throws IOException {
            String line = readLine();
            final int extension = line.indexOf(';');
            if (extension >= 0) {
                line = line.substring(0, extension);
            }
            try {
                final long size = Long.parseLong(line.trim(), 16);
                if (size < 0) {
                    throw new IOException("provider response chunk size is invalid");
                }
                return size;
            } catch (NumberFormatException error) {
                throw new IOException("provider response chunk size is invalid");
            }
        }

        private String readLine() throws IOException {
            final ByteArrayOutputStream line = new ByteArrayOutputStream();
            while (true) {
                final int value = input.read();
                if (value < 0) {
                    throw new IOException("provider response body ended early");
                }
                if (value == '\n') {
                    final String text = new String(line.toByteArray(), StandardCharsets.ISO_8859_1);
                    return text.endsWith("\r") ? text.substring(0, text.length() - 1) : text;
                }
                if (line.size() > 4096) {
                    throw new IOException("provider response chunk line is too long");
                }
                line.write(value);
            }
        }
    }
}
