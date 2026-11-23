// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Blocking HTTP/1.1 transport over `std::net` and rustls.
//!
//! It never logs, follows redirects, reuses connections, or retries. Each request opens one
//! connection, writes the request once with `Connection: close`, and streams the response
//! body under the same absolute deadline.

use std::io::{self, BufRead, BufReader, Read, Write};
use std::net::{IpAddr, Ipv6Addr, SocketAddr, TcpStream, ToSocketAddrs};
use std::sync::{Arc, mpsc};
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
}

impl BlockingHttp1Transport {
    /// Creates a transport that trusts the Mozilla roots from `webpki-roots`.
    pub(crate) fn new(policy: AddressPolicy) -> Self {
        Self {
            policy,
            tls_configuration: build_tls_configuration().map(Arc::new),
        }
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
        let deadline = Instant::now() + timeout.min(MAXIMUM_REQUEST_TIMEOUT);
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
        let resolved = resolve_host(&target.host, target.port, deadline).ok_or_else(not_sent)?;
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
        let body =
            response_body(reader, &request.method, status, &headers).map_err(possibly_sent)?;
        Ok(ProviderHttpResponse::new(
            status,
            ResponseHeaders::new(headers),
            body,
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

/// Resolves `host` within the deadline; `None` for a failed, empty, or late answer.
fn resolve_host(host: &str, port: u16, deadline: Instant) -> Option<Vec<SocketAddr>> {
    if let Ok(address) = host.parse::<IpAddr>() {
        return Some(vec![SocketAddr::new(address, port)]);
    }
    let (sender, receiver) = mpsc::channel();
    let lookup_host = host.to_string();
    // The system resolver cannot time out, so a detached thread runs it under the deadline.
    thread::Builder::new()
        .name("dex-provider-dns".to_string())
        .spawn(move || {
            let answer = (lookup_host.as_str(), port)
                .to_socket_addrs()
                .map(Iterator::collect::<Vec<SocketAddr>>);
            // The requester may have stopped waiting at its deadline.
            let _receiver_gone = sender.send(answer);
        })
        .ok()?;
    let answer = receiver
        .recv_timeout(deadline.saturating_duration_since(Instant::now()))
        .ok()?
        .ok()?;
    (!answer.is_empty()).then_some(answer)
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

/// Chooses the body framing from the method, status, and headers.
fn response_body(
    reader: BufReader<ProviderConnection>,
    method: &str,
    status: u16,
    headers: &[(String, String)],
) -> io::Result<Box<dyn Read + Send>> {
    if method.eq_ignore_ascii_case("HEAD") || status == 204 || status == 304 {
        return Ok(Box::new(io::empty()));
    }
    if let Some(final_coding) = header_list_values(headers, "transfer-encoding").last() {
        if final_coding.eq_ignore_ascii_case("chunked") {
            return Ok(Box::new(ChunkedBody {
                reader,
                remaining_in_chunk: 0,
                is_finished: false,
            }));
        }
        return Ok(Box::new(reader));
    }
    let mut content_length = None;
    for value in header_list_values(headers, "content-length") {
        let parsed = Some(value)
            .filter(|value| value.bytes().all(|byte| byte.is_ascii_digit()))
            .and_then(|value| value.parse::<u64>().ok())
            .ok_or_else(|| invalid_response("provider Content-Length is invalid"))?;
        if content_length.is_some_and(|previous| previous != parsed) {
            return Err(invalid_response("provider Content-Length values conflict"));
        }
        content_length = Some(parsed);
    }
    match content_length {
        Some(length) => Ok(Box::new(ContentLengthBody {
            reader,
            remaining: length,
        })),
        None => Ok(Box::new(reader)),
    }
}

/// Returns the nonempty comma-separated elements of every `name` header, in order.
fn header_list_values<'headers>(
    headers: &'headers [(String, String)],
    name: &'headers str,
) -> impl Iterator<Item = &'headers str> {
    headers
        .iter()
        .filter(move |(candidate, _)| candidate.eq_ignore_ascii_case(name))
        .flat_map(|(_, value)| value.split(','))
        .map(str::trim)
        .filter(|value| !value.is_empty())
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
    use super::*;

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
