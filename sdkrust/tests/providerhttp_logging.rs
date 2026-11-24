// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Provider requests leave no secret in `log` or `tracing` output at the most verbose level.
//!
//! This binary installs process-wide loggers, so it holds exactly one test.

use std::fmt::Write as _;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

use dex_connectors_sdk::providerhttp::{
    AddressPolicy, ProviderHttpClient, ProviderHttpRequest, read_bounded_body,
};

const TEST_SECRET: &str = "inert-test-secret-7f3c";

/// Everything every installed logger received.
type CapturedOutput = Arc<Mutex<String>>;

struct CapturingLogger {
    output: CapturedOutput,
}

impl log::Log for CapturingLogger {
    fn enabled(&self, _metadata: &log::Metadata<'_>) -> bool {
        true
    }

    fn log(&self, record: &log::Record<'_>) {
        let mut output = self.output.lock().expect("log output lock");
        let _infallible = writeln!(
            output,
            "{} {} {}",
            record.level(),
            record.target(),
            record.args()
        );
    }

    fn flush(&self) {}
}

struct CapturingSubscriber {
    output: CapturedOutput,
    next_span_id: AtomicU64,
}

struct FieldRecorder<'output> {
    output: &'output mut String,
}

impl tracing::field::Visit for FieldRecorder<'_> {
    fn record_debug(&mut self, field: &tracing::field::Field, value: &dyn std::fmt::Debug) {
        let _infallible = write!(self.output, " {}={:?}", field.name(), value);
    }

    fn record_str(&mut self, field: &tracing::field::Field, value: &str) {
        let _infallible = write!(self.output, " {}={}", field.name(), value);
    }
}

impl CapturingSubscriber {
    fn record(
        &self,
        prefix: &str,
        metadata: &tracing::Metadata<'_>,
        visit: impl FnOnce(&mut FieldRecorder<'_>),
    ) {
        let mut output = self.output.lock().expect("tracing output lock");
        let _infallible = write!(output, "{prefix} {} {}", metadata.target(), metadata.name());
        visit(&mut FieldRecorder {
            output: &mut output,
        });
        output.push('\n');
    }
}

impl tracing::Subscriber for CapturingSubscriber {
    fn enabled(&self, _metadata: &tracing::Metadata<'_>) -> bool {
        true
    }

    fn max_level_hint(&self) -> Option<tracing::level_filters::LevelFilter> {
        Some(tracing::level_filters::LevelFilter::TRACE)
    }

    fn new_span(&self, attributes: &tracing::span::Attributes<'_>) -> tracing::span::Id {
        self.record("span", attributes.metadata(), |recorder| {
            attributes.record(recorder);
        });
        tracing::span::Id::from_u64(self.next_span_id.fetch_add(1, Ordering::Relaxed))
    }

    fn record(&self, _span: &tracing::span::Id, values: &tracing::span::Record<'_>) {
        let mut output = self.output.lock().expect("tracing output lock");
        values.record(&mut FieldRecorder {
            output: &mut output,
        });
        output.push('\n');
    }

    fn record_follows_from(&self, _span: &tracing::span::Id, _follows: &tracing::span::Id) {}

    fn event(&self, event: &tracing::Event<'_>) {
        self.record("event", event.metadata(), |recorder| event.record(recorder));
    }

    fn enter(&self, _span: &tracing::span::Id) {}

    fn exit(&self, _span: &tracing::span::Id) {}
}

fn read_request_head(stream: &mut std::net::TcpStream) -> Vec<u8> {
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

#[test]
fn secrets_in_headers_and_query_never_reach_debug_logs() {
    let output = CapturedOutput::default();
    log::set_boxed_logger(Box::new(CapturingLogger {
        output: Arc::clone(&output),
    }))
    .expect("install the log logger");
    log::set_max_level(log::LevelFilter::Trace);
    tracing::subscriber::set_global_default(CapturingSubscriber {
        output: Arc::clone(&output),
        next_span_id: AtomicU64::new(1),
    })
    .expect("install the tracing subscriber");
    log::debug!("log capture probe");
    tracing::debug!("tracing capture probe");

    let listener = TcpListener::bind("127.0.0.1:0").expect("bind loopback");
    let port = listener.local_addr().expect("local address").port();
    let server = thread::spawn(move || {
        let mut heads = Vec::new();
        for (index, stream) in listener.incoming().take(2).enumerate() {
            let mut stream = stream.expect("accepted connection");
            heads.push(read_request_head(&mut stream));
            if index == 0 {
                let _ignored_closed_client = stream
                    .write_all(b"HTTP/1.1 401 Unauthorized\r\nContent-Length: 6\r\n\r\ndenied");
            }
            // The second connection closes without a response to cover the error path.
        }
        heads
    });
    let client = ProviderHttpClient::new(Duration::from_secs(5), AddressPolicy::PrivateNetworkHttp)
        .expect("client");
    // `localhost` exercises DNS resolution and the address policy as well.
    let url = format!("http://localhost:{port}/v1/items?api_key={TEST_SECRET}");
    let request = ProviderHttpRequest::new("POST", url)
        .header("X-Api-Key", TEST_SECRET)
        .header("Authorization", format!("Bearer {TEST_SECRET}"))
        .body(format!("{{\"token\":\"{TEST_SECRET}\"}}"));
    let mut response = client.send_traced(&request).expect("response");
    assert_eq!(response.status(), 401);
    assert_eq!(
        read_bounded_body(&mut response, 64).expect("body"),
        b"denied"
    );
    let error = client
        .send_traced(&request)
        .expect_err("dropped connection");
    let heads = server.join().expect("server thread");

    for head in &heads {
        let head = String::from_utf8_lossy(head);
        assert!(
            head.contains(&format!("?api_key={TEST_SECRET} HTTP/1.1")),
            "{head}"
        );
        assert!(
            head.contains(&format!("\r\nX-Api-Key: {TEST_SECRET}\r\n")),
            "{head}"
        );
    }
    for rendered in [
        format!("{request:?}"),
        format!("{response:?}"),
        format!("{error:?}"),
        error.to_string(),
    ] {
        assert!(!rendered.contains(TEST_SECRET), "{rendered}");
    }
    let captured = output.lock().expect("captured output").clone();
    assert!(captured.contains("log capture probe"), "{captured}");
    assert!(captured.contains("tracing capture probe"), "{captured}");
    assert!(!captured.contains(TEST_SECRET), "{captured}");
}
