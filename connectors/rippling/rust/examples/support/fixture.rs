// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_rippling::{Client, Credentials};
use dex_connectors_sdk::{ConnectionRef, StaticCredentialProvider, providerhttp::*};
use serde_json::json;
use std::{
    collections::HashMap,
    error::Error,
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
    time::Duration,
};

pub struct RipplingFixture {
    provider: Arc<WorkersApi>,
}
impl RipplingFixture {
    pub fn new(operation: &'static str) -> Self {
        Self {
            provider: Arc::new(WorkersApi {
                operation,
                reads: 0.into(),
            }),
        }
    }
    pub fn client(&self) -> Result<Arc<Client>, Box<dyn Error>> {
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            ConnectionRef::new("rippling", "rippling"),
            Credentials::new("test-bearer"),
        )])));
        let http =
            ProviderHttpClient::with_transport(self.provider.clone(), Duration::from_secs(15))?;
        Ok(Arc::new(Client::with_http_client(
            "https://rest.ripplingapis.com",
            1 << 20,
            credentials,
            http,
        )?))
    }
    pub fn verify_reads(&self) {
        assert_eq!(self.provider.reads.load(Ordering::SeqCst), 2);
    }
}
struct WorkersApi {
    operation: &'static str,
    reads: AtomicUsize,
}
impl ProviderHttpTransport for WorkersApi {
    fn send(
        &self,
        request: &ProviderHttpRequest,
        _: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        assert_eq!(request.method, "GET");
        assert!(
            request
                .headers
                .iter()
                .any(|(name, value)| name.eq_ignore_ascii_case("Authorization")
                    && value == "Bearer test-bearer")
        );
        let lookup = self.operation == "GetWorker";
        let path = if lookup {
            "worker-123/"
        } else {
            "?cursor=a%2B%3D%26&limit=2"
        };
        assert_eq!(
            request.url,
            format!("https://rest.ripplingapis.com/workers/{path}")
        );
        let worker = json!({"id":"worker-123","title":"Engineer","status":null,"privateField":42});
        let body = if lookup {
            worker
        } else {
            json!({"results":[worker],"next_link":"?cursor=later%2B%2F%3D"})
        };
        let status = if self.reads.fetch_add(1, Ordering::SeqCst) == 0 {
            503
        } else {
            200
        };
        Ok(ProviderHttpResponse::new(
            status,
            ResponseHeaders::new(vec![]),
            Box::new(std::io::Cursor::new(serde_json::to_vec(&body).unwrap())),
        ))
    }
}
