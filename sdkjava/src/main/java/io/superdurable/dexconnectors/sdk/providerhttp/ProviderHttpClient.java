// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import io.superdurable.dexconnectors.sdk.singledispatch.DispatchObservation;
import java.io.IOException;
import java.io.InterruptedIOException;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.Proxy;
import java.net.Socket;
import java.net.URI;
import java.net.URISyntaxException;
import java.net.UnknownHostException;
import java.security.GeneralSecurityException;
import java.security.KeyStore;
import java.time.Duration;
import java.time.Instant;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.regex.Pattern;
import javax.net.SocketFactory;
import javax.net.ssl.HttpsURLConnection;
import javax.net.ssl.TrustManager;
import javax.net.ssl.TrustManagerFactory;
import javax.net.ssl.X509TrustManager;
import okhttp3.Call;
import okhttp3.ConnectionPool;
import okhttp3.Dns;
import okhttp3.EventListener;
import okhttp3.Headers;
import okhttp3.HttpUrl;
import okhttp3.Interceptor;
import okhttp3.OkHttpClient;
import okhttp3.Protocol;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;
import okhttp3.ResponseBody;

/**
 * Redirect-safe provider HTTP client with an optional address policy, built on OkHttp.
 *
 * <p>The client never follows redirects, never caches, and never retries a request: a response
 * OkHttp would follow up, such as a 503 with {@code Retry-After: 0}, is returned to the caller
 * instead, with a body of at most 1 MiB. Method names,
 * header names, and header values are validated before any DNS lookup or dial, and the request
 * URL is the ASCII form of the URI. The host is resolved once before the call: a restricted
 * {@link AddressPolicy} checks every answer and dials only those addresses, directly, without any
 * HTTP or SOCKS proxy. {@link AddressPolicy#UNRESTRICTED} uses the system {@code ProxySelector} on
 * every path, including single-dispatch sends. TLS uses the JVM default
 * {@code HttpsURLConnection} socket factory, so an application's installed trust configuration
 * applies, with OkHttp's strict hostname verification and SNI.
 *
 * <p>Each call has an absolute call timeout: the request timeout, shortened to the remaining
 * single-dispatch write window inside a Fence. Inside a Fence, the client registers the call's
 * non-blocking {@code cancel()} with the Fence, which runs it before any application abort action,
 * and checks the dispatch right before the request bytes are written. A refusal before that point,
 * or a DNS, connect, or TLS failure before any request bytes, is provably not sent.
 *
 * <p>Each request clears the enclosing single-dispatch observation before it starts and records
 * the response status when headers arrive. Instances are immutable and thread-safe; they share one
 * OkHttp connection pool.
 */
public final class ProviderHttpClient implements ProviderHttpTransport {
    private static final Pattern TOKEN = Pattern.compile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$");

    private static final long MAXIMUM_FOLLOW_UP_BODY_BYTES = 1 << 20;
    private static final Set<String> SDK_MANAGED_HEADERS = new HashSet<>(Arrays.asList(
            "host", "content-length", "transfer-encoding", "connection", "keep-alive", "proxy-connection",
            "te", "trailer", "upgrade", "expect"));
    private static final ExecutorService DNS_LOOKUPS = Executors.newCachedThreadPool(runnable -> {
        final Thread thread = new Thread(runnable, "dex-connector-provider-dns");
        thread.setDaemon(true);
        return thread;
    });

    // Tests pause here to cancel between the connection and the first request byte.
    static volatile Runnable beforeRequestWriteForTests;
    // Tests pause here, after the last active check and before the first request byte.
    static volatile Runnable afterActiveCheckForTests;

    private final Duration requestTimeout;
    private final AddressPolicy addressPolicy;
    private final OkHttpClient baseClient;

    /**
     * Creates a client.
     *
     * @param requestTimeout the positive absolute timeout of each request, connect through body
     * @param addressPolicy the address policy
     * @throws IllegalArgumentException if the timeout is not positive or the policy is missing
     */
    public ProviderHttpClient(final Duration requestTimeout, final AddressPolicy addressPolicy) {
        if (requestTimeout == null || requestTimeout.isZero() || requestTimeout.isNegative()) {
            throw new IllegalArgumentException("provider request timeout must be positive");
        }
        if (addressPolicy == null) {
            throw new IllegalArgumentException("address policy is invalid");
        }
        this.requestTimeout = requestTimeout;
        this.addressPolicy = addressPolicy;
        final OkHttpClient.Builder builder = new OkHttpClient.Builder()
                .followRedirects(false)
                .followSslRedirects(false)
                .retryOnConnectionFailure(false)
                .cache(null)
                .sslSocketFactory(HttpsURLConnection.getDefaultSSLSocketFactory(), defaultTrustManager());
        if (addressPolicy != AddressPolicy.UNRESTRICTED) {
            // Each call checks its own DNS answers, so its connection cannot be reused; keep none idle.
            builder.proxy(Proxy.NO_PROXY).socketFactory(new DirectSocketFactory())
                    .connectionPool(new ConnectionPool(0, 1, TimeUnit.SECONDS));
        }
        this.baseClient = builder.build();
    }

    /**
     * Creates an unrestricted client with a 30-second request timeout.
     */
    public ProviderHttpClient() {
        this(Duration.ofSeconds(30), AddressPolicy.UNRESTRICTED);
    }

    /**
     * Returns the default request timeout.
     *
     * @return the timeout
     */
    public Duration getRequestTimeout() {
        return requestTimeout;
    }

    /**
     * Returns the address policy.
     *
     * @return the policy
     */
    public AddressPolicy getAddressPolicy() {
        return addressPolicy;
    }

    /**
     * Sends a request once without following redirects and returns the streaming response.
     *
     * @param request the request
     * @return the response, owned by the caller
     * @throws IllegalArgumentException if the URL, method, or a header is invalid; nothing was sent
     * @throws AddressRefusalException if the address policy refused the request; nothing was sent
     * @throws ProviderTransportException if DNS, connect, TLS, or the exchange failed
     * @throws IOException if the request failed in another way
     */
    @Override
    public ProviderHttpResponse send(final ProviderHttpRequest request) throws IOException {
        final URI uri;
        try {
            uri = new URI(request.getUrl());
        } catch (URISyntaxException error) {
            throw new IllegalArgumentException("provider URL is invalid");
        }
        final String scheme = uri.getScheme() == null ? "" : uri.getScheme().toLowerCase(Locale.ROOT);
        if (!"http".equals(scheme) && !"https".equals(scheme)) {
            if (addressPolicy != AddressPolicy.UNRESTRICTED) {
                throw new AddressRefusalException("URL scheme is not allowed");
            }
            throw new IllegalArgumentException("provider URL scheme must be http or https");
        }
        final HttpUrl url = HttpUrl.parse(uri.toASCIIString());
        if (url == null || uri.getHost() == null || uri.getHost().isEmpty()) {
            throw new IllegalArgumentException("provider URL is invalid");
        }
        if (request.getMethod() == null || !TOKEN.matcher(request.getMethod()).matches()) {
            throw new IllegalArgumentException("provider request method is invalid");
        }
        final String method = request.getMethod().toUpperCase(Locale.ROOT);
        final Headers headers = validatedHeaders(request.getHeaders());
        final RequestBody body = requestBody(method, request.getBody());
        DispatchObservation.clearResponseStatus();
        // One monotonic budget from here covers DNS, connect, request, and response.
        final long budgetDeadlineNanos = System.nanoTime()
                + boundByWriteDeadline(request.getTimeout() == null ? requestTimeout : request.getTimeout()).toNanos();
        final Dns dns = policyDns(url.host(), scheme, budgetDeadlineNanos);
        final long remainingMillis = remainingMillis(budgetDeadlineNanos);
        final SendProgress progress = new SendProgress();
        final OkHttpClient.Builder builder = baseClient.newBuilder()
                .dns(dns)
                .callTimeout(remainingMillis, TimeUnit.MILLISECONDS)
                .connectTimeout(remainingMillis, TimeUnit.MILLISECONDS)
                .readTimeout(remainingMillis, TimeUnit.MILLISECONDS)
                .writeTimeout(remainingMillis, TimeUnit.MILLISECONDS)
                .eventListener(progress)
                .addNetworkInterceptor(new SingleNetworkRequest())
                .addNetworkInterceptor(new DispatchActiveCheck());
        if (DispatchObservation.currentWriteDeadline() != null) {
            // An HTTP/2 stream can write HEADERS after cancel() returns; HTTP/1.1 cancel closes the socket first.
            builder.protocols(Collections.singletonList(Protocol.HTTP_1_1));
        }
        final Call call = builder.build().newCall(
                new Request.Builder().url(url).headers(headers).method(method, body).build());
        // The Fence cancels the call itself, before any application abort action, at its deadline or on Dex cancellation.
        DispatchObservation.registerTransportCancellation(call::cancel);
        final Response response;
        try {
            response = call.execute();
        } catch (DispatchInactiveException refused) {
            throw new ProviderTransportException(true, null, null);
        } catch (FollowUpBlockedException blocked) {
            DispatchObservation.recordResponseStatus(blocked.firstResponse.getStatusCode());
            return blocked.firstResponse;
        } catch (IOException error) {
            throw new ProviderTransportException(!progress.hasStartedRequest(), null, null);
        }
        final int statusCode = response.code();
        DispatchObservation.recordResponseStatus(statusCode);
        final ProviderHttpHeaders responseHeaders = new ProviderHttpHeaders(response.headers().toMultimap());
        return new ProviderHttpResponse(statusCode, responseHeaders, response.body().byteStream(), response::close);
    }

    /**
     * Resolves a restricted host once, before the call, and serves only those checked answers.
     *
     * <p>An unrestricted client resolves through the system, or not at all behind an HTTP proxy.
     */
    private Dns policyDns(final String host, final String scheme, final long budgetDeadlineNanos) throws IOException {
        if (addressPolicy == AddressPolicy.UNRESTRICTED) {
            return Dns.SYSTEM;
        }
        // JVM DNS cannot be cancelled, so the wait for it is bounded by the request budget instead.
        final Future<List<InetAddress>> lookup =
                DNS_LOOKUPS.submit(() -> AddressPolicyRules.resolveCheckedAddresses(host, scheme, addressPolicy));
        final List<InetAddress> addresses;
        try {
            addresses = lookup.get(Math.max(0, budgetDeadlineNanos - System.nanoTime()), TimeUnit.NANOSECONDS);
        } catch (TimeoutException expired) {
            lookup.cancel(true);
            throw new ProviderTransportException(true, null, null);
        } catch (InterruptedException interrupted) {
            lookup.cancel(true);
            Thread.currentThread().interrupt();
            throw new ProviderTransportException(true, null, null);
        } catch (ExecutionException failed) {
            if (failed.getCause() instanceof AddressRefusalException) {
                throw (AddressRefusalException) failed.getCause();
            }
            throw new ProviderTransportException(true, null, null);
        }
        return hostname -> {
            if (!hostname.equals(host)) {
                throw new UnknownHostException("provider host changed during the request");
            }
            return addresses;
        };
    }

    private static Headers validatedHeaders(final Map<String, String> values) {
        final Headers.Builder builder = new Headers.Builder();
        for (Map.Entry<String, String> header : values.entrySet()) {
            final String name = header.getKey();
            final String value = header.getValue();
            if (name == null || !TOKEN.matcher(name).matches()) {
                throw new IllegalArgumentException("provider request header name is invalid");
            }
            // The transport derives these from the validated URL and the actual body.
            if (SDK_MANAGED_HEADERS.contains(name.toLowerCase(Locale.ROOT))) {
                throw new IllegalArgumentException("provider request header " + name + " is managed by the SDK");
            }
            if (value == null || !isHeaderValue(value)) {
                throw new IllegalArgumentException("provider request header value is invalid");
            }
            builder.add(name, value);
        }
        return builder.build();
    }

    // RFC 9110 field-value: visible ASCII, space, and tab, without leading or trailing whitespace.
    private static boolean isHeaderValue(final String value) {
        if (!value.isEmpty() && (isWhitespace(value.charAt(0)) || isWhitespace(value.charAt(value.length() - 1)))) {
            return false;
        }
        for (int index = 0; index < value.length(); index++) {
            final char character = value.charAt(index);
            if (character != '\t' && (character < 0x20 || character > 0x7e)) {
                return false;
            }
        }
        return true;
    }

    private static boolean isWhitespace(final char character) {
        return character == ' ' || character == '\t';
    }

    private static RequestBody requestBody(final String method, final byte[] body) {
        if (body != null) {
            if ("GET".equals(method) || "HEAD".equals(method)) {
                throw new IllegalArgumentException("provider request method does not permit a body");
            }
            return RequestBody.create(body, null);
        }
        // OkHttp requires a body for these methods; an empty one sends Content-Length: 0.
        final boolean isBodyRequired = "POST".equals(method) || "PUT".equals(method) || "PATCH".equals(method)
                || "PROPPATCH".equals(method) || "REPORT".equals(method);
        return isBodyRequired ? RequestBody.create(new byte[0], null) : null;
    }

    /**
     * Bounds a timeout by the remaining time of the enclosing Fence write window.
     *
     * @throws ProviderTransportException as provably not sent once the window has passed
     */
    private static Duration boundByWriteDeadline(final Duration timeout) throws ProviderTransportException {
        if (!DispatchObservation.isActive()) {
            throw new ProviderTransportException(true, null, null);
        }
        final Instant writeDeadline = DispatchObservation.currentWriteDeadline();
        if (writeDeadline == null) {
            return timeout;
        }
        final Duration remaining = Duration.between(Instant.now(), writeDeadline);
        if (remaining.isZero() || remaining.isNegative()) {
            throw new ProviderTransportException(true, null, null);
        }
        return remaining.compareTo(timeout) < 0 ? remaining : timeout;
    }

    /**
     * Returns the positive whole milliseconds left in the budget, rounding a sub-millisecond rest up.
     *
     * @throws ProviderTransportException as provably not sent once the budget is spent
     */
    static long remainingMillis(final long budgetDeadlineNanos) throws ProviderTransportException {
        final long remainingNanos = budgetDeadlineNanos - System.nanoTime();
        if (remainingNanos <= 0) {
            throw new ProviderTransportException(true, null, null);
        }
        // OkHttp reads zero as no timeout, so a positive rest is never truncated to it.
        return Math.max(1, TimeUnit.NANOSECONDS.toMillis(remainingNanos + 999_999));
    }

    private static X509TrustManager defaultTrustManager() {
        try {
            final TrustManagerFactory factory = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
            factory.init((KeyStore) null);
            for (TrustManager manager : factory.getTrustManagers()) {
                if (manager instanceof X509TrustManager) {
                    return (X509TrustManager) manager;
                }
            }
        } catch (GeneralSecurityException error) {
            throw new IllegalStateException("the JVM default trust manager is unavailable", error);
        }
        throw new IllegalStateException("the JVM default trust manager is unavailable");
    }

    /** Runs on the calling thread right before OkHttp writes the request on its connection. */
    private static final class DispatchActiveCheck implements Interceptor {
        @Override
        public Response intercept(final Chain chain) throws IOException {
            final Runnable pause = beforeRequestWriteForTests;
            if (pause != null) {
                pause.run();
            }
            if (!DispatchObservation.isActive()) {
                throw new DispatchInactiveException();
            }
            final Runnable pauseAfterCheck = afterActiveCheckForTests;
            if (pauseAfterCheck != null) {
                pauseAfterCheck.run();
            }
            return chain.proceed(chain.request());
        }
    }

    /**
     * Lets a call reach the network once; OkHttp's response follow-ups, such as a 503 with
     * {@code Retry-After: 0}, would otherwise send the request again.
     */
    private static final class SingleNetworkRequest implements Interceptor {
        private ProviderHttpResponse firstResponse;

        @Override
        public Response intercept(final Chain chain) throws IOException {
            if (firstResponse != null) {
                // The caller gets the original response and decides about another send through Dex.
                throw new FollowUpBlockedException(firstResponse);
            }
            final Response response = chain.proceed(chain.request());
            if (isFollowUpCandidate(response.code())) {
                final ResponseBody peeked = response.peekBody(MAXIMUM_FOLLOW_UP_BODY_BYTES);
                firstResponse = ProviderHttpResponse.buffered(response.code(),
                        new ProviderHttpHeaders(response.headers().toMultimap()), peeked.bytes());
            }
            return response;
        }

        private static boolean isFollowUpCandidate(final int statusCode) {
            return statusCode == 401 || statusCode == 407 || statusCode == 408 || statusCode == 421
                    || statusCode == 503 || (statusCode >= 300 && statusCode < 400);
        }
    }

    /** OkHttp tried to send a follow-up request that the SDK refuses. */
    private static final class FollowUpBlockedException extends IOException {
        private static final long serialVersionUID = 1L;

        private final transient ProviderHttpResponse firstResponse;

        private FollowUpBlockedException(final ProviderHttpResponse firstResponse) {
            super("provider response follow-up refused");
            this.firstResponse = firstResponse;
        }
    }

    /** The single-dispatch write window ended or Dex cancelled the attempt before any request bytes. */
    private static final class DispatchInactiveException extends InterruptedIOException {
        private static final long serialVersionUID = 1L;

        private DispatchInactiveException() {
            super("single-dispatch send is no longer active");
        }
    }

    /** Records whether OkHttp began writing the request, which separates not-sent failures. */
    private static final class SendProgress extends EventListener {
        private final AtomicBoolean hasStartedRequest = new AtomicBoolean();

        @Override
        public void requestHeadersStart(final Call call) {
            hasStartedRequest.set(true);
        }

        boolean hasStartedRequest() {
            return hasStartedRequest.get();
        }
    }

    /** Creates sockets that bypass any JVM SOCKS proxy, for restricted address policies. */
    private static final class DirectSocketFactory extends SocketFactory {
        @Override
        public Socket createSocket() {
            return new Socket(Proxy.NO_PROXY);
        }

        @Override
        public Socket createSocket(final String host, final int port) throws IOException {
            final Socket socket = createSocket();
            socket.connect(new InetSocketAddress(host, port));
            return socket;
        }

        @Override
        public Socket createSocket(final String host, final int port, final InetAddress localHost, final int localPort)
                throws IOException {
            final Socket socket = createSocket();
            socket.bind(new InetSocketAddress(localHost, localPort));
            socket.connect(new InetSocketAddress(host, port));
            return socket;
        }

        @Override
        public Socket createSocket(final InetAddress host, final int port) throws IOException {
            final Socket socket = createSocket();
            socket.connect(new InetSocketAddress(host, port));
            return socket;
        }

        @Override
        public Socket createSocket(final InetAddress address, final int port, final InetAddress localAddress,
                final int localPort) throws IOException {
            final Socket socket = createSocket();
            socket.bind(new InetSocketAddress(localAddress, localPort));
            socket.connect(new InetSocketAddress(address, port));
            return socket;
        }
    }
}
