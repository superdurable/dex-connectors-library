// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Provider HTTP client behavior against local TCP servers.

use std::collections::BTreeMap;
use std::io::{self, Read, Write};
use std::net::{Shutdown, TcpListener, TcpStream};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use dex_connectors_sdk::providerhttp::{
    AddressPolicy, ProviderHttpClient, ProviderHttpError, ProviderHttpRequest,
    ProviderHttpResponse, ResponseHeaders, encode_query_values, is_header_safe_credential,
    parse_retry_after, read_bounded_body, read_error_tokens, validate_base_url,
};

fn read_request_head(stream: &mut TcpStream) -> Vec<u8> {
    let mut received = Vec::new();
    let mut buffer = [0_u8; 1024];
    while !received.windows(4).any(|window| window == b"\r\n\r\n") {
        match stream.read(&mut buffer) {
            Ok(0) | Err(_) => break,
            Ok(count) => received.extend_from_slice(&buffer[..count]),
        }
    }
    received
}

/// Serves `connections` requests with `handler` on a loopback port.
fn serve(connections: usize, handler: fn(TcpStream)) -> u16 {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind loopback");
    let port = listener.local_addr().expect("local address").port();
    thread::spawn(move || {
        for stream in listener.incoming().take(connections).flatten() {
            handler(stream);
        }
    });
    port
}

fn client(policy: AddressPolicy) -> ProviderHttpClient {
    ProviderHttpClient::new(Duration::from_secs(2), policy).expect("client")
}

#[test]
fn redirect_is_not_followed_and_body_is_bounded() {
    let port = serve(1, |mut stream| {
        read_request_head(&mut stream);
        let response = b"HTTP/1.1 302 Found\r\nLocation: https://other.example/steal\r\nContent-Length: 5\r\nConnection: close\r\n\r\n12345";
        stream.write_all(response).expect("write response");
    });
    let request = ProviderHttpRequest::new("GET", format!("http://127.0.0.1:{port}/resource"))
        .header("Authorization", "Bearer fake");
    let mut response = client(AddressPolicy::Unrestricted)
        .send_traced(&request)
        .expect("redirect response");
    assert_eq!(response.status(), 302);
    let Err(ProviderHttpError::BodyTooLarge(oversized)) = read_bounded_body(&mut response, 4)
    else {
        panic!("expected an oversized body");
    };
    assert_eq!(oversized.status, 302);
    assert_eq!(
        oversized.headers.get("location"),
        Some("https://other.example/steal")
    );
    assert!(!oversized.to_string().contains("other.example"));
    assert!(!format!("{request:?}").contains("Bearer"));
}

#[test]
fn connection_failures_before_dispatch_are_provably_not_sent() {
    let refused_port = {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind loopback");
        listener.local_addr().expect("local address").port()
    };
    let tls_port = serve(1, |mut stream| {
        let mut buffer = [0_u8; 1024];
        let _ignored_read = stream.read(&mut buffer);
        let _ignored_write =
            stream.write_all(b"HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n");
    });
    let client = client(AddressPolicy::Unrestricted);
    for url in [
        format!("http://127.0.0.1:{refused_port}/"),
        "https://nonexistent.invalid/".to_string(),
        format!("https://127.0.0.1:{tls_port}/"),
    ] {
        let request = ProviderHttpRequest::new("POST", url.clone()).body(b"write".to_vec());
        let error = client.send_traced(&request).expect_err("connection fails");
        let ProviderHttpError::Transport(transport) = &error else {
            panic!("expected a transport error for {url}");
        };
        assert!(transport.provably_not_sent, "{url}");
        assert_eq!(error.to_string(), "provider request failed");
    }
}

#[test]
fn reset_after_request_headers_is_possibly_sent() {
    let port = serve(1, |mut stream| {
        read_request_head(&mut stream);
        let _ignored = stream.shutdown(Shutdown::Both);
    });
    let request = ProviderHttpRequest::new("POST", format!("http://127.0.0.1:{port}/"))
        .body(b"write".to_vec());
    let error = client(AddressPolicy::Unrestricted)
        .send_traced(&request)
        .expect_err("reset fails");
    assert!(!error.is_provably_not_sent());
}

#[test]
fn address_policies_refuse_before_dialing_and_allow_private_http() {
    let request = ProviderHttpRequest::new("GET", "http://127.0.0.1:9/");
    let Err(ProviderHttpError::AddressRefusal(refusal)) =
        client(AddressPolicy::PublicHttps).send_traced(&request)
    else {
        panic!("public HTTPS must refuse plain loopback HTTP");
    };
    assert_eq!(refusal.reason(), "plain http is not allowed; use https");
    let metadata = ProviderHttpRequest::new("GET", "https://169.254.169.254/");
    let Err(ProviderHttpError::AddressRefusal(refusal)) =
        client(AddressPolicy::PrivateNetworkHttp).send_traced(&metadata)
    else {
        panic!("link-local addresses are always refused");
    };
    assert!(refusal.reason().starts_with("link-local"));
    let port = serve(1, |mut stream| {
        read_request_head(&mut stream);
        stream
            .write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
            .expect("write response");
    });
    let allowed = ProviderHttpRequest::new("GET", format!("http://localhost:{port}/"));
    let mut response = client(AddressPolicy::PrivateNetworkHttp)
        .send_traced(&allowed)
        .expect("private HTTP is allowed");
    assert_eq!(read_bounded_body(&mut response, 16).expect("body"), b"ok");
    let ftp = ProviderHttpRequest::new("GET", "ftp://127.0.0.1/");
    assert!(matches!(
        client(AddressPolicy::PrivateNetworkHttp).send_traced(&ftp),
        Err(ProviderHttpError::AddressRefusal(_))
    ));
}

#[test]
fn expired_deadline_is_provably_not_sent() {
    let request = ProviderHttpRequest::new("POST", "http://127.0.0.1:9/");
    let error = client(AddressPolicy::Unrestricted)
        .send_traced_before(&request, std::time::Instant::now())
        .expect_err("expired deadline");
    assert!(error.is_provably_not_sent());
}

struct FailingReader;

impl Read for FailingReader {
    fn read(&mut self, _buffer: &mut [u8]) -> io::Result<usize> {
        Err(io::Error::other("private provider details"))
    }
}

#[test]
fn bounded_reader_keeps_metadata_on_transport_error() {
    let mut response = ProviderHttpResponse::new(
        200,
        ResponseHeaders::new(vec![("X-Request-ID".to_string(), "received".to_string())]),
        Box::new(FailingReader),
    );
    let Err(ProviderHttpError::Transport(error)) = read_bounded_body(&mut response, 4) else {
        panic!("expected a transport error");
    };
    assert_eq!(error.status, Some(200));
    assert_eq!(
        error
            .headers
            .as_ref()
            .and_then(|headers| headers.get("x-request-id")),
        Some("received")
    );
    assert!(!error.to_string().contains("private provider"));
}

#[test]
fn query_values_match_go_url_values_encode() {
    let values = BTreeMap::from([
        (
            "b".to_string(),
            vec!["two words".to_string(), "x&y=z".to_string()],
        ),
        ("a".to_string(), vec!["é/~".to_string()]),
        ("empty".to_string(), Vec::new()),
    ]);
    assert_eq!(
        encode_query_values(&values),
        "a=%C3%A9%2F~&b=two+words&b=x%26y%3Dz"
    );
}

#[test]
fn error_tokens_retry_after_and_base_urls() {
    assert_eq!(
        read_error_tokens(
            br#"{"error":{"code":"rate_limit","message":"secret text"}}"#,
            &["/error/code", "/error/message"]
        ),
        vec!["rate_limit"]
    );
    assert!(read_error_tokens(b"not-json", &["/error/code"]).is_empty());
    assert!(
        read_error_tokens(
            br#"{"error":{"code":"SECRET TOKEN WITH SPACES"}}"#,
            &["/error/code"]
        )
        .is_empty()
    );
    assert_eq!(
        read_error_tokens(br#"{"errors":[{"code":"busy"}]}"#, &["/errors/0/code"]),
        vec!["busy"]
    );
    assert_eq!(
        read_error_tokens(br#"{"value":1.50,"power":1e3}"#, &["/value", "/power"]),
        vec!["1.50", "1e3"]
    );
    let now = UNIX_EPOCH + Duration::from_secs(1_793_232_000);
    assert_eq!(parse_retry_after("99999", now), 3600.0);
    assert_eq!(parse_retry_after("bad", now), 0.0);
    assert_eq!(parse_retry_after("-1", now), 0.0);
    assert_eq!(parse_retry_after("1.5", now), 0.0);
    let later = httpdate::fmt_http_date(now + Duration::from_secs(7));
    assert_eq!(parse_retry_after(&later, now), 7.0);
    let earlier = httpdate::fmt_http_date(now - Duration::from_secs(86_400));
    assert_eq!(parse_retry_after(&earlier, now), 0.0);
    assert_eq!(parse_retry_after("5", SystemTime::now()), 5.0);
    assert_eq!(
        validate_base_url("http://127.0.0.1:8080/").expect("loopback"),
        "http://127.0.0.1:8080"
    );
    assert_eq!(
        validate_base_url("https://gmail.googleapis.com/gmail/v1/").expect("https"),
        "https://gmail.googleapis.com/gmail/v1"
    );
    for invalid in [
        "http://provider.example",
        "https://provider.example?token=",
        "https://@provider.example",
        "https://provider.example:0",
        "https://provider.example:99999",
        "https:// space.example",
    ] {
        assert!(validate_base_url(invalid).is_err(), "{invalid}");
    }
    assert!(is_header_safe_credential("abc|123"));
    assert!(!is_header_safe_credential("abc\n123"));
    assert!(!is_header_safe_credential(""));
}
