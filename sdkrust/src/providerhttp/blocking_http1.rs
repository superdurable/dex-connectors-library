// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Blocking HTTP/1.1 transport over `std::net` and rustls.
//!
//! It never logs, follows redirects, reuses connections, or retries. Each request opens one
//! connection, writes the request once with `Connection: close`, and streams the response
//! body under the same absolute deadline.

use std::collections::HashMap;
use std::io::{self, BufRead, BufReader, Read, Write};
use std::net::{IpAddr, Ipv6Addr, SocketAddr, TcpStream, ToSocketAddrs};
use std::sync::{Arc, Mutex, OnceLock, PoisonError, mpsc};
use std::thread;
use std::time::{Duration, Instant};

use rustls::pki_types::ServerName;
use rustls::{ClientConfig, ClientConnection, RootCertStore, StreamOwned};

use super::address_policy::check_resolved_addresses;
use super::{
    AddressPolicy, AddressRefusal, ProviderHttpError, ProviderHttpRequest, ProviderHttpResponse,
    ProviderHttpTransport, ProviderTransportError, ResponseHeaders,
};

/// Longest single request; a larger timeout is clamped so the deadline cannot overflow.
const MAXIMUM_REQUEST_TIMEOUT: Duration = Duration::from_secs(24 * 60 * 60);
const MAXIMUM_RESPONSE_HEAD_BYTES: usize = 64 << 10;
const MAXIMUM_RESPONSE_HEADER_COUNT: usize = 256;
const MAXIMUM_INTERIM_RESPONSES: usize = 8;
const MAXIMUM_CHUNK_SIZE_LINE_BYTES: usize = 4096;
const MAXIMUM_TRAILER_BYTES: usize = 64 << 10;
const SYSTEM_RESOLVER_WORKER_COUNT: usize = 4;
const SYSTEM_RESOLVER_QUEUE_CAPACITY: usize = 256;
const SYSTEM_RESOLVER_LEAK_THRESHOLD: Duration = Duration::from_secs(30);
const SYSTEM_RESOLVER_MAXIMUM_THREAD_COUNT: usize = 64;
const DEFAULT_USER_AGENT_HEADER: &str = concat!(
    "User-Agent: dex-connectors-sdk/",
    env!("CARGO_PKG_VERSION"),
    "\r\n"
);
/// Headers whose values the transport owns: framing, connection, and the dialed host.
const TRANSPORT_OWNED_HEADERS: [&str; 4] =
    ["host", "content-length", "transfer-encoding", "connection"];

/// One-connection-per-request HTTP/1.1 client with an optional address policy.
pub(crate) struct BlockingHttp1Transport {
    policy: AddressPolicy,
    // `None` when the TLS configuration cannot be built; HTTPS then fails before dialing.
    tls_configuration: Option<Arc<ClientConfig>>,
    resolver_pool: Arc<ResolverPool>,
}

impl BlockingHttp1Transport {
    /// Creates a transport that trusts the Mozilla roots from `webpki-roots`.
    pub(crate) fn new(policy: AddressPolicy) -> Self {
        Self {
            policy,
            tls_configuration: build_tls_configuration().map(Arc::new),
            resolver_pool: system_resolver_pool(),
        }
    }

    #[cfg(test)]
    fn with_resolver_pool(policy: AddressPolicy, resolver_pool: Arc<ResolverPool>) -> Self {
        Self {
            policy,
            tls_configuration: build_tls_configuration().map(Arc::new),
            resolver_pool,
        }
    }

    /// Resolves `host` within the deadline; `None` for a failed, empty, late, or refused lookup.
    fn resolve_host(&self, host: &str, port: u16, deadline: Instant) -> Option<Vec<SocketAddr>> {
        if let Ok(address) = host.parse::<IpAddr>() {
            return Some(vec![SocketAddr::new(address, port)]);
        }
        self.resolver_pool.resolve_before(host, port, deadline)
    }

    fn handshake(
        &self,
        mut socket: DeadlineTcpStream,
        host: &str,
    ) -> Option<StreamOwned<ClientConnection, DeadlineTcpStream>> {
        let configuration = self.tls_configuration.clone()?;
        let server_name = ServerName::try_from(host.to_string()).ok()?;
        let mut connection = ClientConnection::new(configuration, server_name).ok()?;
        // Completing the handshake before writing keeps handshake failures provably unsent.
        while connection.is_handshaking() {
            connection.complete_io(&mut socket).ok()?;
        }
        Some(StreamOwned::new(connection, socket))
    }
}

impl ProviderHttpTransport for BlockingHttp1Transport {
    fn send(
        &self,
        request: &ProviderHttpRequest,
        timeout: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        self.send_before(
            request,
            Instant::now() + timeout.min(MAXIMUM_REQUEST_TIMEOUT),
        )
    }

    fn send_before(
        &self,
        request: &ProviderHttpRequest,
        deadline: Instant,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        let deadline = deadline.min(Instant::now() + MAXIMUM_REQUEST_TIMEOUT);
        let not_sent = || ProviderHttpError::Transport(ProviderTransportError::new(true));
        let scheme = request
            .url
            .split_once("://")
            .map(|(scheme, _)| scheme.to_ascii_lowercase())
            .unwrap_or_default();
        if self.policy != AddressPolicy::Unrestricted && scheme != "http" && scheme != "https" {
            return Err(ProviderHttpError::AddressRefusal(AddressRefusal::new(
                "URL scheme is not allowed",
            )));
        }
        let target = RequestTarget::parse(&request.url).ok_or_else(not_sent)?;
        let mut encoded_request = encode_request_head(request, &target).ok_or_else(not_sent)?;
        encoded_request.extend_from_slice(&request.body);
        let resolved = self
            .resolve_host(&target.host, target.port, deadline)
            .ok_or_else(not_sent)?;
        let addresses = check_resolved_addresses(resolved, &scheme, self.policy)
            .map_err(ProviderHttpError::AddressRefusal)?;
        let stream = connect_first(&addresses, deadline).ok_or_else(not_sent)?;
        // Disabling Nagle only affects latency, so a failure is harmless.
        let _nagle_unchanged = stream.set_nodelay(true);
        let socket = DeadlineTcpStream { stream, deadline };
        let mut connection = if target.is_tls {
            ProviderConnection::Tls(Box::new(
                self.handshake(socket, &target.host).ok_or_else(not_sent)?,
            ))
        } else {
            ProviderConnection::Plain(socket)
        };
        // From here on, request bytes may have reached the provider.
        let possibly_sent =
            |_: io::Error| ProviderHttpError::Transport(ProviderTransportError::new(false));
        connection
            .write_all(&encoded_request)
            .and_then(|()| connection.flush())
            .map_err(possibly_sent)?;
        let mut reader = BufReader::new(connection);
        let (status, headers) = read_response_head(&mut reader).map_err(possibly_sent)?;
        let headers = ResponseHeaders::new(headers);
        // A status-only caller must not treat a misframed response as success.
        let Ok(framing) = choose_response_framing(&request.method, status, headers.entries())
        else {
            return Err(ProviderHttpError::Transport(ProviderTransportError {
                provably_not_sent: false,
                status: Some(status),
                headers: Some(headers),
            }));
        };
        Ok(ProviderHttpResponse::new(
            status,
            headers,
            response_body(reader, framing),
        ))
    }
}

fn build_tls_configuration() -> Option<ClientConfig> {
    let roots = RootCertStore {
        roots: webpki_roots::TLS_SERVER_ROOTS.to_vec(),
    };
    // An explicit provider avoids depending on a process-wide default provider.
    let mut configuration =
        ClientConfig::builder_with_provider(Arc::new(rustls::crypto::ring::default_provider()))
            .with_safe_default_protocol_versions()
            .ok()?
            .with_root_certificates(roots)
            .with_no_client_auth();
    configuration.alpn_protocols = vec![b"http/1.1".to_vec()];
    Some(configuration)
}

/// Origin and request target of an absolute `http` or `https` URL.
struct RequestTarget {
    is_tls: bool,
    /// Hostname or IP literal without IPv6 brackets, used for DNS and TLS.
    host: String,
    port: u16,
    host_header: String,
    path_and_query: String,
}

impl RequestTarget {
    /// Parses the URL; user information, non-ASCII hosts, and unknown schemes are rejected.
    fn parse(url: &str) -> Option<Self> {
        let (scheme, rest) = url.split_once("://")?;
        let (is_tls, default_port) = match scheme.to_ascii_lowercase().as_str() {
            "http" => (false, 80),
            "https" => (true, 443),
            _ => return None,
        };
        let without_fragment = rest.split_once('#').map_or(rest, |(before, _)| before);
        let authority_end = without_fragment
            .find(['/', '?'])
            .unwrap_or(without_fragment.len());
        let (authority, path_and_query) = without_fragment.split_at(authority_end);
        if authority.is_empty() || authority.contains('@') {
            return None;
        }
        let (host, host_in_authority, port_text) =
            if let Some(bracketed) = authority.strip_prefix('[') {
                let (host, after) = bracketed.split_once(']')?;
                host.parse::<Ipv6Addr>().ok()?;
                let port_text = match after {
                    "" => None,
                    _ => Some(after.strip_prefix(':')?),
                };
                (host, &authority[..host.len() + 2], port_text)
            } else {
                let (host, port_text) = match authority.split_once(':') {
                    Some((host, port_text)) => (host, Some(port_text)),
                    None => (authority, None),
                };
                let is_valid_host = !host.is_empty()
                    && host.bytes().all(|byte| {
                        byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'-' | b'_')
                    });
                if !is_valid_host {
                    return None;
                }
                (host, host, port_text)
            };
        let port = match port_text {
            None | Some("") => default_port,
            Some(text) if text.bytes().all(|byte| byte.is_ascii_digit()) => {
                text.parse::<u16>().ok().filter(|port| *port != 0)?
            }
            Some(_) => return None,
        };
        let host_header = if port == default_port {
            host_in_authority.to_string()
        } else {
            format!("{host_in_authority}:{port}")
        };
        let mut encoded_path = String::with_capacity(path_and_query.len() + 1);
        if !path_and_query.starts_with('/') {
            encoded_path.push('/');
        }
        // Spaces, controls, and non-ASCII bytes are percent-encoded so they cannot split lines.
        for byte in path_and_query.bytes() {
            if (0x21..=0x7e).contains(&byte) {
                encoded_path.push(char::from(byte));
            } else {
                encoded_path.push_str(&format!("%{byte:02X}"));
            }
        }
        Some(Self {
            is_tls,
            host: host.to_string(),
            port,
            host_header,
            path_and_query: encoded_path,
        })
    }
}

fn is_http_token(value: &str) -> bool {
    !value.is_empty()
        && value.bytes().all(|byte| {
            byte.is_ascii_alphanumeric()
                || matches!(
                    byte,
                    b'!' | b'#'
                        | b'$'
                        | b'%'
                        | b'&'
                        | b'\''
                        | b'*'
                        | b'+'
                        | b'-'
                        | b'.'
                        | b'^'
                        | b'_'
                        | b'`'
                        | b'|'
                        | b'~'
                )
        })
}

fn is_header_value_safe(value: &str) -> bool {
    value
        .bytes()
        .all(|byte| byte == b'\t' || (byte >= 0x20 && byte != 0x7f))
}

/// Encodes the request line and headers; `None` for a method or header that could inject.
fn encode_request_head(request: &ProviderHttpRequest, target: &RequestTarget) -> Option<Vec<u8>> {
    if !is_http_token(&request.method) {
        return None;
    }
    let mut head = format!(
        "{} {} HTTP/1.1\r\nHost: {}\r\n",
        request.method, target.path_and_query, target.host_header
    );
    for (name, value) in &request.headers {
        if !is_http_token(name) || !is_header_value_safe(value) {
            return None;
        }
        let is_transport_owned = TRANSPORT_OWNED_HEADERS
            .iter()
            .any(|owned| name.eq_ignore_ascii_case(owned));
        if !is_transport_owned {
            head.push_str(name);
            head.push_str(": ");
            head.push_str(value);
            head.push_str("\r\n");
        }
    }
    // Some providers reject requests without a User-Agent, as Go always sends one.
    let has_user_agent = request
        .headers
        .iter()
        .any(|(name, _)| name.eq_ignore_ascii_case("user-agent"));
    if !has_user_agent {
        head.push_str(DEFAULT_USER_AGENT_HEADER);
    }
    let is_body_method = ["POST", "PUT", "PATCH"]
        .iter()
        .any(|method| request.method.eq_ignore_ascii_case(method));
    if !request.body.is_empty() || is_body_method {
        head.push_str(&format!("Content-Length: {}\r\n", request.body.len()));
    }
    head.push_str("Connection: close\r\n\r\n");
    Some(head.into_bytes())
}

/// Blocking hostname lookup: the system resolver in production, a fake in tests.
pub(crate) trait HostResolver: Send + Sync {
    /// Returns every address of `host` with `port`.
    fn resolve_host(&self, host: &str, port: u16) -> io::Result<Vec<SocketAddr>>;
}

struct SystemHostResolver;

impl HostResolver for SystemHostResolver {
    fn resolve_host(&self, host: &str, port: u16) -> io::Result<Vec<SocketAddr>> {
        (host, port).to_socket_addrs().map(Iterator::collect)
    }
}

/// One process-wide pool, so every transport shares the same bounded resolver threads.
fn system_resolver_pool() -> Arc<ResolverPool> {
    static SYSTEM_RESOLVER_POOL: OnceLock<Arc<ResolverPool>> = OnceLock::new();
    SYSTEM_RESOLVER_POOL
        .get_or_init(|| {
            Arc::new(ResolverPool::new(
                Arc::new(SystemHostResolver),
                ResolverPoolLimits {
                    worker_count: SYSTEM_RESOLVER_WORKER_COUNT,
                    queue_capacity: SYSTEM_RESOLVER_QUEUE_CAPACITY,
                    leak_threshold: SYSTEM_RESOLVER_LEAK_THRESHOLD,
                    maximum_thread_count: SYSTEM_RESOLVER_MAXIMUM_THREAD_COUNT,
                },
            ))
        })
        .clone()
}

type ResolutionAnswer = io::Result<Vec<SocketAddr>>;

struct ResolutionTask {
    host: String,
    port: u16,
    deadline: Instant,
    answer_sender: mpsc::SyncSender<ResolutionAnswer>,
}

/// Sizing of a [`ResolverPool`].
#[derive(Clone, Copy, Debug)]
pub(crate) struct ResolverPoolLimits {
    /// Workers kept available for new lookups.
    pub(crate) worker_count: usize,
    /// Lookups waiting for a worker; a full queue fails at once.
    pub(crate) queue_capacity: usize,
    /// Lookup age after which its worker counts as leaked and gets a replacement.
    pub(crate) leak_threshold: Duration,
    /// Hard cap on resolver threads, leaked ones included.
    pub(crate) maximum_thread_count: usize,
}

/// Thread accounting shared by the pool and its workers.
#[derive(Debug, Default)]
struct ResolverPoolState {
    next_worker_id: u64,
    thread_count: usize,
    most_thread_count: usize,
    /// Start time of each worker's in-progress lookup.
    lookup_started_at: HashMap<u64, Instant>,
}

impl ResolverPoolState {
    /// Threads not stuck in a lookup for at least `leak_threshold`.
    fn available_thread_count(&self, leak_threshold: Duration, now: Instant) -> usize {
        let leaked_thread_count = self
            .lookup_started_at
            .values()
            .filter(|started_at| now.saturating_duration_since(**started_at) >= leak_threshold)
            .count();
        self.thread_count.saturating_sub(leaked_thread_count)
    }
}

/// Lazily started resolver threads with a bounded queue; expired lookups are abandoned.
///
/// A system lookup cannot be cancelled, so a worker stuck past the leak threshold is replaced
/// while the thread total stays within the hard cap.
pub(crate) struct ResolverPool {
    resolver: Arc<dyn HostResolver>,
    limits: ResolverPoolLimits,
    task_sender: mpsc::SyncSender<ResolutionTask>,
    task_receiver: Arc<Mutex<mpsc::Receiver<ResolutionTask>>>,
    state: Arc<Mutex<ResolverPoolState>>,
}

impl ResolverPool {
    /// Creates a pool whose threads start on the first lookup.
    pub(crate) fn new(resolver: Arc<dyn HostResolver>, limits: ResolverPoolLimits) -> Self {
        let (task_sender, task_receiver) = mpsc::sync_channel(limits.queue_capacity);
        Self {
            resolver,
            limits,
            task_sender,
            task_receiver: Arc::new(Mutex::new(task_receiver)),
            state: Arc::new(Mutex::new(ResolverPoolState::default())),
        }
    }

    /// Resolves within the deadline; `None` for a full queue or a failed, empty, or late answer.
    fn resolve_before(&self, host: &str, port: u16, deadline: Instant) -> Option<Vec<SocketAddr>> {
        if Instant::now() >= deadline {
            return None;
        }
        self.start_missing_workers();
        let (answer_sender, answer_receiver) = mpsc::sync_channel(1);
        let task = ResolutionTask {
            host: host.to_string(),
            port,
            deadline,
            answer_sender,
        };
        // A full queue fails at once instead of letting abandoned lookups accumulate.
        self.task_sender.try_send(task).ok()?;
        // Measured after startup and enqueueing, so neither extends the deadline.
        let answer = loop {
            let now = Instant::now();
            if now >= deadline {
                return None;
            }
            // A queued lookup re-checks capacity when a busy worker may have leaked.
            let wake_at = self
                .next_replacement_check_at(now)
                .map_or(deadline, |check_at| check_at.min(deadline));
            match answer_receiver.recv_timeout(wake_at.saturating_duration_since(now)) {
                Ok(answer) => break answer,
                Err(mpsc::RecvTimeoutError::Timeout) if Instant::now() < deadline => {
                    self.start_missing_workers();
                }
                Err(_) => return None,
            }
        };
        if Instant::now() >= deadline {
            return None;
        }
        let answer = answer.ok()?;
        (!answer.is_empty()).then_some(answer)
    }

    /// Next time a busy worker crosses the leak threshold, else one threshold from `now`.
    fn next_replacement_check_at(&self, now: Instant) -> Option<Instant> {
        let leak_threshold = self.limits.leak_threshold;
        let state = self.state.lock().unwrap_or_else(PoisonError::into_inner);
        state
            .lookup_started_at
            .values()
            .filter_map(|started_at| started_at.checked_add(leak_threshold))
            .filter(|leaked_at| *leaked_at > now)
            .min()
            .or_else(|| now.checked_add(leak_threshold))
    }

    /// Starts workers until enough are available, never exceeding the thread cap.
    fn start_missing_workers(&self) {
        let first_worker_id;
        let missing_worker_count;
        {
            let mut state = self.state.lock().unwrap_or_else(PoisonError::into_inner);
            let available_thread_count =
                state.available_thread_count(self.limits.leak_threshold, Instant::now());
            missing_worker_count = self
                .limits
                .worker_count
                .saturating_sub(available_thread_count)
                .min(
                    self.limits
                        .maximum_thread_count
                        .saturating_sub(state.thread_count),
                );
            first_worker_id = state.next_worker_id;
            state.next_worker_id += missing_worker_count as u64;
            state.thread_count += missing_worker_count;
            state.most_thread_count = state.most_thread_count.max(state.thread_count);
        }
        // Spawning outside the lock lets a failed spawn's dropped worker release its slot.
        for worker_id in first_worker_id..first_worker_id + missing_worker_count as u64 {
            let worker = ResolverWorker {
                worker_id,
                resolver: Arc::clone(&self.resolver),
                limits: self.limits,
                task_receiver: Arc::clone(&self.task_receiver),
                state: Arc::clone(&self.state),
                is_counted: true,
            };
            // A failed spawn drops the worker; with no workers, every lookup fails as not sent.
            let _spawn_result = thread::Builder::new()
                .name("dex-provider-dns".to_string())
                .spawn(move || worker.run());
        }
    }

    #[cfg(test)]
    fn state(&self) -> Arc<Mutex<ResolverPoolState>> {
        Arc::clone(&self.state)
    }
}

/// One resolver thread; dropping it, on exit or failed spawn, releases its thread slot.
struct ResolverWorker {
    worker_id: u64,
    resolver: Arc<dyn HostResolver>,
    limits: ResolverPoolLimits,
    task_receiver: Arc<Mutex<mpsc::Receiver<ResolutionTask>>>,
    state: Arc<Mutex<ResolverPoolState>>,
    /// Whether this worker still holds a slot in `thread_count`.
    is_counted: bool,
}

impl ResolverWorker {
    fn run(mut self) {
        loop {
            // The lock guard ends with this statement, so a hanging lookup blocks one worker.
            let next_task = self
                .task_receiver
                .lock()
                .unwrap_or_else(PoisonError::into_inner)
                .recv();
            let Ok(task) = next_task else {
                return;
            };
            if Instant::now() >= task.deadline {
                continue;
            }
            self.lock_state()
                .lookup_started_at
                .insert(self.worker_id, Instant::now());
            let answer = self.resolver.resolve_host(&task.host, task.port);
            // The requester may have stopped waiting at its deadline.
            let _requester_gone = task.answer_sender.try_send(answer);
            if self.finish_lookup_and_release_surplus_slot() {
                return;
            }
        }
    }

    /// Ends the lookup; returns `true` after releasing this slot when replacements suffice.
    fn finish_lookup_and_release_surplus_slot(&mut self) -> bool {
        let mut state = self.lock_state();
        state.lookup_started_at.remove(&self.worker_id);
        let is_surplus = state.available_thread_count(self.limits.leak_threshold, Instant::now())
            > self.limits.worker_count;
        if is_surplus {
            state.thread_count -= 1;
        }
        drop(state);
        if is_surplus {
            self.is_counted = false;
        }
        is_surplus
    }

    fn lock_state(&self) -> std::sync::MutexGuard<'_, ResolverPoolState> {
        self.state.lock().unwrap_or_else(PoisonError::into_inner)
    }
}

impl Drop for ResolverWorker {
    fn drop(&mut self) {
        if self.is_counted {
            let mut state = self.lock_state();
            state.thread_count -= 1;
            state.lookup_started_at.remove(&self.worker_id);
        }
    }
}

fn connect_first(addresses: &[SocketAddr], deadline: Instant) -> Option<TcpStream> {
    for address in addresses {
        let remaining = deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            return None;
        }
        if let Ok(stream) = TcpStream::connect_timeout(address, remaining) {
            return Some(stream);
        }
    }
    None
}

/// TCP stream whose every read and write ends by the request deadline.
struct DeadlineTcpStream {
    stream: TcpStream,
    deadline: Instant,
}

impl DeadlineTcpStream {
    fn remaining(&self) -> io::Result<Duration> {
        let remaining = self.deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            return Err(io::Error::new(
                io::ErrorKind::TimedOut,
                "provider request deadline passed",
            ));
        }
        Ok(remaining)
    }
}

impl Read for DeadlineTcpStream {
    fn read(&mut self, buffer: &mut [u8]) -> io::Result<usize> {
        self.stream.set_read_timeout(Some(self.remaining()?))?;
        self.stream.read(buffer)
    }
}

impl Write for DeadlineTcpStream {
    fn write(&mut self, buffer: &[u8]) -> io::Result<usize> {
        self.stream.set_write_timeout(Some(self.remaining()?))?;
        self.stream.write(buffer)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.stream.flush()
    }
}

enum ProviderConnection {
    Plain(DeadlineTcpStream),
    Tls(Box<StreamOwned<ClientConnection, DeadlineTcpStream>>),
}

impl Read for ProviderConnection {
    fn read(&mut self, buffer: &mut [u8]) -> io::Result<usize> {
        match self {
            ProviderConnection::Plain(stream) => stream.read(buffer),
            ProviderConnection::Tls(stream) => stream.read(buffer),
        }
    }
}

impl Write for ProviderConnection {
    fn write(&mut self, buffer: &[u8]) -> io::Result<usize> {
        match self {
            ProviderConnection::Plain(stream) => stream.write(buffer),
            ProviderConnection::Tls(stream) => stream.write(buffer),
        }
    }

    fn flush(&mut self) -> io::Result<()> {
        match self {
            ProviderConnection::Plain(stream) => stream.flush(),
            ProviderConnection::Tls(stream) => stream.flush(),
        }
    }
}

fn invalid_response(message: &'static str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, message)
}

/// Reads one LF-terminated line within `budget` bytes and strips the line ending.
fn read_bounded_line(reader: &mut impl BufRead, budget: &mut usize) -> io::Result<Vec<u8>> {
    let mut line = Vec::new();
    let limit = u64::try_from(*budget).unwrap_or(u64::MAX);
    reader.by_ref().take(limit).read_until(b'\n', &mut line)?;
    *budget = budget.saturating_sub(line.len());
    if line.last() != Some(&b'\n') {
        return Err(invalid_response(
            "provider response line is unterminated or too long",
        ));
    }
    line.pop();
    if line.last() == Some(&b'\r') {
        line.pop();
    }
    Ok(line)
}

/// Reads the final status and headers, skipping interim 1xx responses.
fn read_response_head(reader: &mut impl BufRead) -> io::Result<(u16, Vec<(String, String)>)> {
    let mut budget = MAXIMUM_RESPONSE_HEAD_BYTES;
    for _ in 0..=MAXIMUM_INTERIM_RESPONSES {
        let status_line = read_bounded_line(reader, &mut budget)?;
        let status = parse_status_line(&status_line)?;
        let headers = read_header_lines(reader, &mut budget)?;
        match status {
            101 => return Err(invalid_response("provider switched protocols")),
            100..=199 => continue,
            _ => return Ok((status, headers)),
        }
    }
    Err(invalid_response("provider sent too many interim responses"))
}

fn parse_status_line(line: &[u8]) -> io::Result<u16> {
    let invalid = || invalid_response("provider status line is invalid");
    let rest = line.strip_prefix(b"HTTP/1.").ok_or_else(invalid)?;
    let is_valid = rest.len() >= 5
        && rest[0].is_ascii_digit()
        && rest[1] == b' '
        && rest[2..5].iter().all(u8::is_ascii_digit)
        && (rest.len() == 5 || rest[5] == b' ');
    if !is_valid {
        return Err(invalid());
    }
    let status = rest[2..5]
        .iter()
        .fold(0_u16, |status, digit| status * 10 + u16::from(digit - b'0'));
    if status < 100 {
        return Err(invalid());
    }
    Ok(status)
}

fn read_header_lines(
    reader: &mut impl BufRead,
    budget: &mut usize,
) -> io::Result<Vec<(String, String)>> {
    let mut headers = Vec::new();
    loop {
        let line = read_bounded_line(reader, budget)?;
        if line.is_empty() {
            return Ok(headers);
        }
        if headers.len() == MAXIMUM_RESPONSE_HEADER_COUNT {
            return Err(invalid_response("provider sent too many headers"));
        }
        let colon = line
            .iter()
            .position(|byte| *byte == b':')
            .ok_or_else(|| invalid_response("provider header line is invalid"))?;
        let name = std::str::from_utf8(&line[..colon])
            .ok()
            .filter(|name| is_http_token(name))
            .ok_or_else(|| invalid_response("provider header name is invalid"))?;
        let value = String::from_utf8_lossy(&line[colon + 1..])
            .trim_matches([' ', '\t'])
            .to_string();
        headers.push((name.to_string(), value));
    }
}

/// How the response body ends.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum ResponseFraming {
    Empty,
    ContentLength(u64),
    Chunked,
    UntilClose,
}

/// Validates framing headers, then chooses the body framing; RFC 9112 rejects TE with CL.
fn choose_response_framing(
    method: &str,
    status: u16,
    headers: &[(String, String)],
) -> Result<ResponseFraming, &'static str> {
    let header_values = |name: &'static str| {
        headers
            .iter()
            .filter(move |(candidate, _)| candidate.eq_ignore_ascii_case(name))
            .map(|(_, value)| value.as_str())
    };
    let transfer_codings: Vec<&str> = header_values("transfer-encoding")
        .flat_map(|value| value.split(','))
        .map(str::trim)
        .collect();
    let is_chunked = !transfer_codings.is_empty();
    if is_chunked {
        if transfer_codings.len() != 1 || !transfer_codings[0].eq_ignore_ascii_case("chunked") {
            return Err("provider Transfer-Encoding is not exactly chunked");
        }
        if header_values("content-length").next().is_some() {
            return Err("provider sent both Transfer-Encoding and Content-Length");
        }
    }
    let mut content_length = None;
    for value in header_values("content-length") {
        let parsed = Some(value)
            .filter(|value| !value.is_empty() && value.bytes().all(|byte| byte.is_ascii_digit()))
            .and_then(|value| value.parse::<u64>().ok())
            .ok_or("provider Content-Length is invalid")?;
        if content_length.is_some_and(|previous| previous != parsed) {
            return Err("provider Content-Length values conflict");
        }
        content_length = Some(parsed);
    }
    if method.eq_ignore_ascii_case("HEAD") || status == 204 || status == 304 {
        return Ok(ResponseFraming::Empty);
    }
    if is_chunked {
        return Ok(ResponseFraming::Chunked);
    }
    Ok(content_length.map_or(ResponseFraming::UntilClose, ResponseFraming::ContentLength))
}

/// Builds the streamed body for validated framing.
fn response_body(
    reader: BufReader<ProviderConnection>,
    framing: ResponseFraming,
) -> Box<dyn Read + Send> {
    match framing {
        ResponseFraming::Empty => Box::new(io::empty()),
        ResponseFraming::ContentLength(length) => Box::new(ContentLengthBody {
            reader,
            remaining: length,
        }),
        ResponseFraming::Chunked => Box::new(ChunkedBody {
            reader,
            remaining_in_chunk: 0,
            is_finished: false,
        }),
        ResponseFraming::UntilClose => Box::new(reader),
    }
}

/// Body of exactly `remaining` bytes; an early close is an error, not a short body.
struct ContentLengthBody<R> {
    reader: R,
    remaining: u64,
}

impl<R: Read> Read for ContentLengthBody<R> {
    fn read(&mut self, buffer: &mut [u8]) -> io::Result<usize> {
        if self.remaining == 0 || buffer.is_empty() {
            return Ok(0);
        }
        let wanted = buffer
            .len()
            .min(usize::try_from(self.remaining).unwrap_or(usize::MAX));
        let count = self.reader.read(&mut buffer[..wanted])?;
        if count == 0 {
            return Err(io::Error::new(
                io::ErrorKind::UnexpectedEof,
                "provider closed before the full body",
            ));
        }
        self.remaining -= count as u64;
        Ok(count)
    }
}

/// Chunked transfer coding decoder with bounded size lines and trailers.
struct ChunkedBody<R> {
    reader: R,
    remaining_in_chunk: u64,
    is_finished: bool,
}

impl<R: BufRead> ChunkedBody<R> {
    /// Starts the next chunk; returns `false` after the last chunk and its trailers.
    fn begin_next_chunk(&mut self) -> io::Result<bool> {
        let mut budget = MAXIMUM_CHUNK_SIZE_LINE_BYTES;
        let line = read_bounded_line(&mut self.reader, &mut budget)?;
        let size_text = line
            .split(|byte| *byte == b';')
            .next()
            .unwrap_or_default()
            .trim_ascii();
        let is_valid_size =
            (1..=16).contains(&size_text.len()) && size_text.iter().all(u8::is_ascii_hexdigit);
        let size = std::str::from_utf8(size_text)
            .ok()
            .filter(|_| is_valid_size)
            .and_then(|text| u64::from_str_radix(text, 16).ok())
            .ok_or_else(|| invalid_response("provider chunk size is invalid"))?;
        if size > 0 {
            self.remaining_in_chunk = size;
            return Ok(true);
        }
        let mut trailer_budget = MAXIMUM_TRAILER_BYTES;
        while !read_bounded_line(&mut self.reader, &mut trailer_budget)?.is_empty() {}
        Ok(false)
    }
}

impl<R: BufRead> Read for ChunkedBody<R> {
    fn read(&mut self, buffer: &mut [u8]) -> io::Result<usize> {
        if self.is_finished || buffer.is_empty() {
            return Ok(0);
        }
        if self.remaining_in_chunk == 0 && !self.begin_next_chunk()? {
            self.is_finished = true;
            return Ok(0);
        }
        let wanted = buffer
            .len()
            .min(usize::try_from(self.remaining_in_chunk).unwrap_or(usize::MAX));
        let count = self.reader.read(&mut buffer[..wanted])?;
        if count == 0 {
            return Err(io::Error::new(
                io::ErrorKind::UnexpectedEof,
                "provider closed inside a chunk",
            ));
        }
        self.remaining_in_chunk -= count as u64;
        if self.remaining_in_chunk == 0 {
            let mut budget = 2;
            if !read_bounded_line(&mut self.reader, &mut budget)?.is_empty() {
                return Err(invalid_response("provider chunk is not terminated"));
            }
        }
        Ok(count)
    }
}

#[cfg(test)]
mod tests {
    use std::sync::atomic::{AtomicUsize, Ordering};

    use super::*;

    /// Host the hanging resolver answers at once.
    const FAST_HOST: &str = "fast.example";

    fn decode_chunked(encoded: &[u8]) -> io::Result<Vec<u8>> {
        let mut body = ChunkedBody {
            reader: encoded,
            remaining_in_chunk: 0,
            is_finished: false,
        };
        let mut decoded = Vec::new();
        body.read_to_end(&mut decoded)?;
        Ok(decoded)
    }

    #[test]
    fn chunked_bodies_decode_and_reject_truncation() {
        let decoded =
            decode_chunked(b"4;name=value\r\nWiki\r\n5\r\npedia\r\n0\r\nX-Trailer: 1\r\n\r\n")
                .expect("valid chunked body");
        assert_eq!(decoded, b"Wikipedia");
        assert!(decode_chunked(b"4\r\nWi").is_err());
        assert!(decode_chunked(b"4\r\nWikiXX").is_err());
        assert!(decode_chunked(b"zz\r\n").is_err());
        assert!(decode_chunked(b"4\r\nWiki\r\n").is_err());
    }

    fn framing_of(
        method: &str,
        status: u16,
        headers: &[(&str, &str)],
    ) -> Result<ResponseFraming, &'static str> {
        let headers: Vec<(String, String)> = headers
            .iter()
            .map(|(name, value)| ((*name).to_string(), (*value).to_string()))
            .collect();
        choose_response_framing(method, status, &headers)
    }

    /// Method, status, headers, and the framing they select.
    type FramingCase = (
        &'static str,
        u16,
        &'static [(&'static str, &'static str)],
        ResponseFraming,
    );

    #[test]
    fn response_framing_accepts_supported_forms() {
        let cases: [FramingCase; 8] = [
            (
                "GET",
                200,
                &[("Content-Length", "5")],
                ResponseFraming::ContentLength(5),
            ),
            (
                "GET",
                200,
                &[("Content-Length", "5"), ("content-length", "5")],
                ResponseFraming::ContentLength(5),
            ),
            (
                "GET",
                200,
                &[("Transfer-Encoding", "Chunked")],
                ResponseFraming::Chunked,
            ),
            ("GET", 200, &[], ResponseFraming::UntilClose),
            (
                "HEAD",
                200,
                &[("Content-Length", "99")],
                ResponseFraming::Empty,
            ),
            ("GET", 204, &[], ResponseFraming::Empty),
            (
                "GET",
                304,
                &[("Transfer-Encoding", "chunked")],
                ResponseFraming::Empty,
            ),
            (
                "GET",
                429,
                &[("Content-Length", "0")],
                ResponseFraming::ContentLength(0),
            ),
        ];
        for (method, status, headers, expected) in cases {
            assert_eq!(
                framing_of(method, status, headers),
                Ok(expected),
                "{headers:?}"
            );
        }
    }

    #[test]
    fn response_framing_rejects_ambiguous_or_unsupported_forms() {
        let cases: [&[(&str, &str)]; 14] = [
            &[("Content-Length", "")],
            &[("Content-Length", "abc")],
            &[("Content-Length", "-1")],
            &[("Content-Length", "+5")],
            &[("Content-Length", "5,")],
            &[("Content-Length", "5, 5")],
            &[("Content-Length", "99999999999999999999999")],
            &[("Content-Length", "5"), ("Content-Length", "6")],
            &[("Transfer-Encoding", "gzip")],
            &[("Transfer-Encoding", "gzip, chunked")],
            &[("Transfer-Encoding", "chunked, chunked")],
            &[
                ("Transfer-Encoding", "chunked"),
                ("Transfer-Encoding", "chunked"),
            ],
            &[("Transfer-Encoding", "")],
            &[("Transfer-Encoding", "chunked"), ("Content-Length", "5")],
        ];
        for headers in cases {
            assert!(framing_of("GET", 200, headers).is_err(), "{headers:?}");
        }
        // Framing headers are validated even when the response carries no body.
        assert!(
            framing_of(
                "HEAD",
                200,
                &[("Content-Length", "5"), ("Content-Length", "6")]
            )
            .is_err()
        );
        assert!(framing_of("GET", 204, &[("Transfer-Encoding", "gzip")]).is_err());
    }

    /// Resolver that blocks, except for [`FAST_HOST`], and records concurrent lookups.
    struct HangingResolver {
        is_released: Mutex<bool>,
        release_signal: std::sync::Condvar,
        active_lookup_count: AtomicUsize,
        most_active_lookup_count: AtomicUsize,
    }

    impl HangingResolver {
        fn release(&self) {
            *self
                .is_released
                .lock()
                .unwrap_or_else(PoisonError::into_inner) = true;
            self.release_signal.notify_all();
        }
    }

    impl HostResolver for HangingResolver {
        fn resolve_host(&self, host: &str, _port: u16) -> io::Result<Vec<SocketAddr>> {
            if host == FAST_HOST {
                return Ok(vec![SocketAddr::from(([127, 0, 0, 1], 9))]);
            }
            let active = self.active_lookup_count.fetch_add(1, Ordering::SeqCst) + 1;
            self.most_active_lookup_count
                .fetch_max(active, Ordering::SeqCst);
            let mut is_released = self
                .is_released
                .lock()
                .unwrap_or_else(PoisonError::into_inner);
            while !*is_released {
                is_released = self
                    .release_signal
                    .wait(is_released)
                    .unwrap_or_else(PoisonError::into_inner);
            }
            drop(is_released);
            self.active_lookup_count.fetch_sub(1, Ordering::SeqCst);
            Ok(vec![SocketAddr::from(([127, 0, 0, 1], 9))])
        }
    }

    /// Releases hung lookups even when an assertion fails first.
    struct ReleaseOnDrop(Arc<HangingResolver>);

    impl Drop for ReleaseOnDrop {
        fn drop(&mut self) {
            self.0.release();
        }
    }

    fn wait_until(description: &str, condition: impl Fn() -> bool) {
        let started = Instant::now();
        while !condition() {
            assert!(
                started.elapsed() < Duration::from_secs(10),
                "timed out waiting for {description}"
            );
            thread::yield_now();
        }
    }

    fn new_hanging_resolver() -> Arc<HangingResolver> {
        Arc::new(HangingResolver {
            is_released: Mutex::new(false),
            release_signal: std::sync::Condvar::new(),
            active_lookup_count: AtomicUsize::new(0),
            most_active_lookup_count: AtomicUsize::new(0),
        })
    }

    fn thread_count_of(state: &Mutex<ResolverPoolState>) -> usize {
        state
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .thread_count
    }

    #[test]
    fn hanging_resolver_keeps_resolver_threads_bounded() {
        const WORKER_COUNT: usize = 2;
        const QUEUE_CAPACITY: usize = 3;
        const REQUEST_COUNT: usize = 20;
        let request_timeout = Duration::from_millis(300);
        let resolver = new_hanging_resolver();
        let release_guard = ReleaseOnDrop(Arc::clone(&resolver));
        let pool = Arc::new(ResolverPool::new(
            Arc::clone(&resolver) as Arc<dyn HostResolver>,
            ResolverPoolLimits {
                worker_count: WORKER_COUNT,
                queue_capacity: QUEUE_CAPACITY,
                leak_threshold: Duration::from_secs(3600),
                maximum_thread_count: WORKER_COUNT,
            },
        ));
        let pool_state = pool.state();
        let live_worker_count = || thread_count_of(&pool_state);
        assert_eq!(live_worker_count(), 0, "workers start lazily");
        let transport = BlockingHttp1Transport::with_resolver_pool(
            AddressPolicy::Unrestricted,
            Arc::clone(&pool),
        );
        let request = ProviderHttpRequest::new("POST", "http://hanging.example/items")
            .body(b"write".to_vec());
        let mut fast_failure_count = 0;
        for _ in 0..REQUEST_COUNT {
            let started = Instant::now();
            let Err(ProviderHttpError::Transport(error)) =
                transport.send(&request, request_timeout)
            else {
                panic!("a hanging lookup must fail as a transport error");
            };
            let elapsed = started.elapsed();
            assert!(error.provably_not_sent);
            assert!(
                elapsed < request_timeout + Duration::from_secs(1),
                "{elapsed:?}"
            );
            if elapsed < request_timeout / 2 {
                fast_failure_count += 1;
            }
            assert!(live_worker_count() <= WORKER_COUNT);
        }
        // Each worker holds one hung lookup and the queue holds abandoned tasks; the rest fail fast.
        assert!(
            fast_failure_count >= REQUEST_COUNT - WORKER_COUNT - QUEUE_CAPACITY,
            "{fast_failure_count}"
        );
        wait_until("every worker to hang", || {
            resolver.active_lookup_count.load(Ordering::SeqCst) == WORKER_COUNT
        });
        assert_eq!(
            resolver.most_active_lookup_count.load(Ordering::SeqCst),
            WORKER_COUNT
        );
        assert_eq!(live_worker_count(), WORKER_COUNT);
        drop(release_guard);
        drop(transport);
        drop(pool);
        wait_until("resolver workers to exit", || live_worker_count() == 0);
        assert_eq!(
            resolver.most_active_lookup_count.load(Ordering::SeqCst),
            WORKER_COUNT
        );
    }

    #[test]
    fn leaked_resolver_workers_are_replaced_up_to_the_thread_cap() {
        const WORKER_COUNT: usize = 2;
        const MAXIMUM_THREAD_COUNT: usize = 4;
        let leak_threshold = Duration::from_millis(400);
        let resolver = new_hanging_resolver();
        let release_guard = ReleaseOnDrop(Arc::clone(&resolver));
        // The queue is large enough that only worker availability decides each outcome.
        let pool = ResolverPool::new(
            Arc::clone(&resolver) as Arc<dyn HostResolver>,
            ResolverPoolLimits {
                worker_count: WORKER_COUNT,
                queue_capacity: 64,
                leak_threshold,
                maximum_thread_count: MAXIMUM_THREAD_COUNT,
            },
        );
        let pool_state = pool.state();
        let resolve_within = |host: &str, timeout: Duration| {
            pool.resolve_before(host, 443, Instant::now() + timeout)
        };
        let hang_workers = |expected_active_count: usize| {
            for _ in 0..WORKER_COUNT {
                assert!(resolve_within("hanging.example", Duration::from_millis(100)).is_none());
            }
            wait_until("workers to hang", || {
                resolver.active_lookup_count.load(Ordering::SeqCst) == expected_active_count
            });
        };
        hang_workers(WORKER_COUNT);
        assert_eq!(thread_count_of(&pool_state), WORKER_COUNT);
        // Elapsed time past the leak threshold is the behavior under test.
        thread::sleep(leak_threshold);
        assert!(
            resolve_within(FAST_HOST, Duration::from_secs(5)).is_some(),
            "replacement workers resolve while leaked lookups never return"
        );
        assert_eq!(thread_count_of(&pool_state), MAXIMUM_THREAD_COUNT);
        hang_workers(MAXIMUM_THREAD_COUNT);
        thread::sleep(leak_threshold);
        assert!(
            resolve_within(FAST_HOST, Duration::from_millis(100)).is_none(),
            "the thread cap stops further replacements"
        );
        {
            let state = pool_state.lock().unwrap_or_else(PoisonError::into_inner);
            assert_eq!(state.thread_count, MAXIMUM_THREAD_COUNT);
            assert_eq!(state.most_thread_count, MAXIMUM_THREAD_COUNT);
        }
        drop(release_guard);
        wait_until("surplus workers to exit", || {
            thread_count_of(&pool_state) == WORKER_COUNT
        });
        assert!(resolve_within(FAST_HOST, Duration::from_secs(5)).is_some());
        drop(pool);
        wait_until("resolver workers to exit", || {
            thread_count_of(&pool_state) == 0
        });
        assert_eq!(
            pool_state
                .lock()
                .unwrap_or_else(PoisonError::into_inner)
                .most_thread_count,
            MAXIMUM_THREAD_COUNT
        );
    }

    #[test]
    fn queued_lookup_gets_a_replacement_without_another_call() {
        const WORKER_COUNT: usize = 4;
        const MAXIMUM_THREAD_COUNT: usize = 6;
        let leak_threshold = Duration::from_secs(2);
        let resolver = new_hanging_resolver();
        let release_guard = ReleaseOnDrop(Arc::clone(&resolver));
        let pool = ResolverPool::new(
            Arc::clone(&resolver) as Arc<dyn HostResolver>,
            ResolverPoolLimits {
                worker_count: WORKER_COUNT,
                queue_capacity: 64,
                leak_threshold,
                maximum_thread_count: MAXIMUM_THREAD_COUNT,
            },
        );
        let pool_state = pool.state();
        for _ in 0..WORKER_COUNT {
            let deadline = Instant::now() + Duration::from_millis(100);
            assert!(
                pool.resolve_before("hanging.example", 443, deadline)
                    .is_none()
            );
        }
        wait_until("every worker to hang", || {
            resolver.active_lookup_count.load(Ordering::SeqCst) == WORKER_COUNT
        });
        assert_eq!(thread_count_of(&pool_state), WORKER_COUNT);
        // Queued before any worker leaks; no later call arrives to trigger a replacement.
        let started = Instant::now();
        let deadline = started + Duration::from_secs(5);
        assert!(
            pool.resolve_before(FAST_HOST, 443, deadline).is_some(),
            "the queued lookup must get a replacement worker"
        );
        assert!(Instant::now() < deadline);
        {
            let state = pool_state.lock().unwrap_or_else(PoisonError::into_inner);
            assert!(state.thread_count <= MAXIMUM_THREAD_COUNT);
            assert!(state.most_thread_count <= MAXIMUM_THREAD_COUNT);
            assert!(state.most_thread_count > WORKER_COUNT);
        }
        drop(release_guard);
        drop(pool);
        wait_until("resolver workers to exit", || {
            thread_count_of(&pool_state) == 0
        });
    }

    #[test]
    fn content_length_body_rejects_early_close() {
        let mut body = ContentLengthBody {
            reader: &b"abc"[..],
            remaining: 5,
        };
        let mut contents = Vec::new();
        assert!(body.read_to_end(&mut contents).is_err());
    }

    #[test]
    fn request_targets_keep_host_and_encode_unsafe_path_bytes() {
        let target =
            RequestTarget::parse("https://API.example:8443/a b?q=é#fragment").expect("valid URL");
        assert!(target.is_tls);
        assert_eq!(target.host, "API.example");
        assert_eq!(target.port, 8443);
        assert_eq!(target.host_header, "API.example:8443");
        assert_eq!(target.path_and_query, "/a%20b?q=%C3%A9");
        let ipv6 = RequestTarget::parse("http://[::1]?x=1").expect("IPv6 URL");
        assert_eq!(ipv6.host, "::1");
        assert_eq!(ipv6.host_header, "[::1]");
        assert_eq!(ipv6.path_and_query, "/?x=1");
        for invalid in [
            "ftp://example.test/",
            "https://user:secret@example.test/",
            "https://example.test:0/",
            "https://exa mple.test/",
            "https:///path",
        ] {
            assert!(RequestTarget::parse(invalid).is_none(), "{invalid}");
        }
    }

    #[test]
    fn request_heads_reject_injection_and_own_framing_headers() {
        let target = RequestTarget::parse("http://127.0.0.1:8080/items").expect("valid URL");
        let request = ProviderHttpRequest::new("DELETE", "http://127.0.0.1:8080/items")
            .header("Host", "attacker.example")
            .header("Content-Length", "99")
            .header("X-Request-Id", "abc");
        let head = String::from_utf8(encode_request_head(&request, &target).expect("valid head"))
            .expect("ASCII head");
        assert_eq!(
            head,
            format!(
                "DELETE /items HTTP/1.1\r\nHost: 127.0.0.1:8080\r\nX-Request-Id: abc\r\n{DEFAULT_USER_AGENT_HEADER}Connection: close\r\n\r\n"
            )
        );
        assert!(head.contains("User-Agent: dex-connectors-sdk/"));
        let custom_agent = ProviderHttpRequest::new("GET", "http://127.0.0.1:8080/items")
            .header("user-agent", "connector/1");
        let head =
            String::from_utf8(encode_request_head(&custom_agent, &target).expect("valid head"))
                .expect("ASCII head");
        assert_eq!(head.matches("ser-agent").count(), 1, "{head}");
        assert!(head.contains("\r\nuser-agent: connector/1\r\n"), "{head}");
        let injected = ProviderHttpRequest::new("GET", "http://127.0.0.1/")
            .header("X-Value", "a\r\nInjected: 1");
        assert!(encode_request_head(&injected, &target).is_none());
        let bad_method = ProviderHttpRequest::new("GET /", "http://127.0.0.1/");
        assert!(encode_request_head(&bad_method, &target).is_none());
    }
}
