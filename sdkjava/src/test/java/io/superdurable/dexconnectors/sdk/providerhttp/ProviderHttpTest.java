// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpServer;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.time.ZoneOffset;
import java.time.format.DateTimeFormatter;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

class ProviderHttpTest {
    @Test
    void queryValuesMatchGoUrlValuesEncode() {
        final Map<String, List<String>> values = new LinkedHashMap<>();
        values.put("z", Collections.singletonList("~ * ! ' ( ) + / ? & = ☃"));
        values.put("a", Arrays.asList("one", "two"));
        assertEquals("a=one&a=two&z=~+%2A+%21+%27+%28+%29+%2B+%2F+%3F+%26+%3D+%E2%98%83",
                ProviderHttp.encodeQueryValues(values));
        final Map<String, List<String>> unicode = new LinkedHashMap<>();
        unicode.put("é", Collections.singletonList("café"));
        unicode.put("a", Collections.singletonList("雪"));
        assertEquals("a=%E9%9B%AA&%C3%A9=caf%C3%A9", ProviderHttp.encodeQueryValues(unicode));
        final Map<String, List<String>> empty = new LinkedHashMap<>();
        empty.put("empty", Collections.<String>emptyList());
        empty.put("x", Collections.singletonList(""));
        assertEquals("x=", ProviderHttp.encodeQueryValues(empty));
    }

    @Test
    void replaysGoAddressPolicyTable() {
        final Object[][] table = {
            {new String[] {"127.0.0.1"}, true, true, false},
            {new String[] {"::1"}, true, true, false},
            {new String[] {"10.1.2.3"}, true, true, false},
            {new String[] {"172.16.0.5"}, true, true, false},
            {new String[] {"172.31.255.255"}, true, true, false},
            {new String[] {"192.168.1.1"}, true, true, false},
            {new String[] {"fc00::1"}, true, true, false},
            {new String[] {"fd12:3456::1"}, true, true, false},
            {new String[] {"10.0.0.1", "192.168.0.2", "::1"}, true, true, false},
            {new String[] {"::ffff:10.0.0.1"}, true, true, false},
            {new String[] {"::ffff:8.8.8.8"}, false, true, true},
            {new String[] {"8.8.8.8"}, false, true, true},
            {new String[] {"2001:4860:4860::8888"}, false, true, true},
            {new String[] {"8.8.8.8", "1.1.1.1"}, false, true, true},
            {new String[] {"100.64.0.1"}, false, true, false},
            {new String[] {"10.0.0.1", "8.8.8.8"}, false, true, false},
            {new String[] {"8.8.8.8", "10.0.0.1"}, false, true, false},
            {new String[] {"169.254.169.254"}, false, false, false},
            {new String[] {"169.254.0.1"}, false, false, false},
            {new String[] {"fe80::1"}, false, false, false},
            {new String[] {"febf::1"}, false, false, false},
            {new String[] {"::ffff:169.254.169.254"}, false, false, false},
            {new String[] {"fd00:ec2::254"}, false, false, false},
            {new String[] {"127.0.0.1", "169.254.169.254"}, false, false, false},
            {new String[] {"8.8.8.8", "169.254.169.254"}, false, false, false},
            {new String[] {"0.0.0.0"}, false, false, false},
            {new String[] {"::"}, false, false, false},
            {new String[] {"224.0.0.1"}, false, false, false},
            {new String[] {"ff02::1"}, false, false, false},
            {new String[] {"255.255.255.255"}, false, false, false},
        };
        for (Object[] row : table) {
            final String[] addresses = (String[]) row[0];
            assertEquals(row[1], isAllowed(addresses, "http", AddressPolicy.PRIVATE_NETWORK_HTTP), Arrays.toString(addresses));
            assertEquals(row[2], isAllowed(addresses, "https", AddressPolicy.PRIVATE_NETWORK_HTTP), Arrays.toString(addresses));
            assertFalse(isAllowed(addresses, "http", AddressPolicy.PUBLIC_HTTPS), Arrays.toString(addresses));
            assertEquals(row[3], isAllowed(addresses, "https", AddressPolicy.PUBLIC_HTTPS), Arrays.toString(addresses));
        }
    }

    @Test
    void restrictedPolicyRefusesBeforeDialing() throws Exception {
        final ProviderHttpClient client = new ProviderHttpClient(Duration.ofSeconds(5), AddressPolicy.PUBLIC_HTTPS);
        assertThrows(AddressRefusalException.class,
                () -> client.send(ProviderHttpRequest.newBuilder("GET", "http://8.8.8.8/").build()));
        assertThrows(AddressRefusalException.class,
                () -> client.send(ProviderHttpRequest.newBuilder("GET", "https://169.254.169.254/").build()));
        assertThrows(AddressRefusalException.class,
                () -> client.send(ProviderHttpRequest.newBuilder("GET", "ftp://example.test/").build()));
    }

    @Test
    void redirectIsNotFollowedAndBodyIsBounded() throws Exception {
        final AtomicInteger requests = new AtomicInteger();
        final HttpServer server = HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0);
        server.createContext("/", exchange -> {
            requests.incrementAndGet();
            exchange.getResponseHeaders().add("Location", "https://other.example/steal");
            final byte[] body = "redirect body".getBytes(StandardCharsets.UTF_8);
            exchange.sendResponseHeaders(302, body.length);
            try (OutputStream output = exchange.getResponseBody()) {
                output.write(body);
            }
        });
        server.start();
        try {
            final ProviderHttpClient client = new ProviderHttpClient(Duration.ofSeconds(5), AddressPolicy.PRIVATE_NETWORK_HTTP);
            final String url = "http://127.0.0.1:" + server.getAddress().getPort() + "/start";
            final ProviderHttpResponse observed = client.send(ProviderHttpRequest.newBuilder("GET", url).build());
            assertEquals(302, observed.getStatusCode());
            assertArrayEquals("redirect body".getBytes(StandardCharsets.UTF_8), observed.readBoundedBody(1024));
            assertEquals(1, requests.get());
            final ProviderHttpResponse oversized = client.send(ProviderHttpRequest.newBuilder("GET", url).build());
            final BodyTooLargeException error = assertThrows(BodyTooLargeException.class, () -> oversized.readBoundedBody(4));
            assertEquals(302, error.getStatusCode());
            assertEquals("https://other.example/steal", error.getHeaders().first("location"));
            assertFalse(error.getMessage().contains("other.example"));
        } finally {
            server.stop(0);
        }
    }

    @Test
    void connectionRefusalIsProvablyNotSent() throws Exception {
        final int port;
        try (ServerSocket socket = new ServerSocket(0, 1, InetAddress.getLoopbackAddress())) {
            port = socket.getLocalPort();
        }
        final ProviderHttpClient client = new ProviderHttpClient(Duration.ofSeconds(5), AddressPolicy.UNRESTRICTED);
        final ProviderTransportException error = assertThrows(ProviderTransportException.class, () -> client.send(
                ProviderHttpRequest.newBuilder("POST", "http://127.0.0.1:" + port + "/secret-bearing")
                        .body("{}".getBytes(StandardCharsets.UTF_8)).build()));
        assertTrue(error.isProvablyNotSent());
        assertFalse(error.getMessage().contains("secret-bearing"));
    }

    @Test
    void resetAfterRequestIsPossiblySent() throws Exception {
        try (ServerSocket server = new ServerSocket(0, 1, InetAddress.getLoopbackAddress())) {
            final Thread acceptor = new Thread(() -> {
                try (Socket socket = server.accept()) {
                    final InputStream input = socket.getInputStream();
                    final byte[] buffer = new byte[256];
                    input.read(buffer);
                    socket.setSoLinger(true, 0);
                } catch (java.io.IOException ignored) {
                    // The client observes the reset.
                }
            });
            acceptor.start();
            final ProviderHttpClient client = new ProviderHttpClient(Duration.ofSeconds(5), AddressPolicy.UNRESTRICTED);
            final ProviderTransportException error = assertThrows(ProviderTransportException.class, () -> client.send(
                    ProviderHttpRequest.newBuilder("POST", "http://127.0.0.1:" + server.getLocalPort() + "/")
                            .body("{}".getBytes(StandardCharsets.UTF_8)).build()));
            assertFalse(error.isProvablyNotSent());
            acceptor.join(5000);
        }
    }

    @Test
    void errorTokensRetryAfterAndBaseUrls() {
        assertEquals(Arrays.asList("rate_limited", "busy"), ProviderHttp.readErrorTokens(
                "{\"error\":{\"code\":\"rate_limited\"},\"errors\":[{\"code\":\"busy\"}]}".getBytes(StandardCharsets.UTF_8),
                Arrays.asList("/error/code", "/errors/0/code", "/missing")));
        assertEquals(Collections.emptyList(), ProviderHttp.readErrorTokens("not-json".getBytes(StandardCharsets.UTF_8),
                Collections.singletonList("/error/code")));
        assertEquals(Collections.emptyList(), ProviderHttp.readErrorTokens(
                "{\"error\":{\"code\":\"SECRET TOKEN WITH SPACES\"}}".getBytes(StandardCharsets.UTF_8),
                Collections.singletonList("/error/code")));
        assertEquals(Arrays.asList("1.50", "1e3"), ProviderHttp.readErrorTokens(
                "{\"value\":1.50,\"power\":1e3}".getBytes(StandardCharsets.UTF_8), Arrays.asList("/value", "/power")));
        final Instant now = Instant.parse("2026-10-29T12:00:00Z");
        assertEquals(Duration.ofHours(1), ProviderHttp.parseRetryAfter("99999", now));
        assertEquals(Duration.ZERO, ProviderHttp.parseRetryAfter("0", now));
        assertEquals(Duration.ofSeconds(1), ProviderHttp.parseRetryAfter("0000001", now));
        assertEquals(Duration.ofHours(1), ProviderHttp.parseRetryAfter("99999999999999999999999", now));
        assertEquals(Duration.ZERO, ProviderHttp.parseRetryAfter("bad", now));
        assertEquals(Duration.ZERO, ProviderHttp.parseRetryAfter("-1", now));
        assertEquals(Duration.ZERO, ProviderHttp.parseRetryAfter("1.5", now));
        assertEquals(Duration.ZERO, ProviderHttp.parseRetryAfter("Thu, 28 Oct 2026 00:00:00 GMT", now));
        assertEquals(Duration.ofSeconds(7), ProviderHttp.parseRetryAfter(DateTimeFormatter
                .ofPattern("EEE, dd MMM yyyy HH:mm:ss 'GMT'", Locale.US)
                .format(now.plusSeconds(7).atOffset(ZoneOffset.UTC)), now));
        assertEquals("http://127.0.0.1:8080", ProviderHttp.validateBaseUrl("http://127.0.0.1:8080/"));
        assertEquals("https://api.example.test/v1", ProviderHttp.validateBaseUrl("https://api.example.test/v1//"));
        assertThrows(IllegalArgumentException.class, () -> ProviderHttp.validateBaseUrl("http://provider.example"));
        assertThrows(IllegalArgumentException.class, () -> ProviderHttp.validateBaseUrl("https://provider.example?token="));
        assertThrows(IllegalArgumentException.class, () -> ProviderHttp.validateBaseUrl("https://@provider.example"));
        assertThrows(IllegalArgumentException.class, () -> ProviderHttp.validateBaseUrl("https://provider.example:0"));
        assertTrue(ProviderHttp.isHeaderSafeCredential("abc|123"));
        assertFalse(ProviderHttp.isHeaderSafeCredential("abc\n123"));
    }

    private static boolean isAllowed(final String[] addresses, final String scheme, final AddressPolicy policy) {
        for (String address : addresses) {
            if (!ProviderHttp.addressRefusalReason(address, scheme, policy).isEmpty()) {
                return false;
            }
        }
        return true;
    }
}
