// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_caiyun as connector;
const CONNECTOR_ID: &str = "caiyun";
const ORIGIN: &str = "https://api.caiyunapp.com";
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        app_key: dex_connectors_sdk::SecretString::new(
            if name == "getCurrentWeather-missing-credentials" {
                ""
            } else {
                "test-app-key"
            },
        ),
        app_secret: dex_connectors_sdk::SecretString::new("test-app-secret"),
    }
}
use connector::GetCurrentWeatherInput as QueryInput;
use dex_connectors_sdk::{CredentialProvider, ValidationError, providerhttp::ProviderHttpClient};
use std::sync::Arc;
use std::time::{Duration, SystemTime};
struct QueryOperation;
impl QueryOperation {
    fn with_http_client_and_clock(
        credentials: Arc<dyn CredentialProvider<connector::Credentials>>,
        http: ProviderHttpClient,
        limit: usize,
        _clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    ) -> Result<connector::GetCurrentWeatherOperation, ValidationError> {
        connector::GetCurrentWeatherOperation::with_signing_sources(
            credentials,
            http,
            ORIGIN,
            if limit == 16 { 8 } else { limit },
            Arc::new(|| SystemTime::UNIX_EPOCH + Duration::from_secs(1792654000)),
            Arc::new(|| {
                Ok([
                    0x01, 0x95, 0xc6, 0x8a, 0x42, 0xe7, 0x72, 0x43, 0xbf, 0xf2, 0xac, 0x97, 0xa7,
                    0x8b, 0x83, 0x7d,
                ])
            }),
        )
    }
}
const OPERATION_ID: &str = "getCurrentWeather";
#[path = "support/http_query.rs"]
mod http_query;

#[test]
fn unavailable_nonce_retries_before_dispatch() {
    use dex_connectors_sdk::providerhttp::{
        ProviderHttpError, ProviderHttpRequest, ProviderHttpResponse, ProviderHttpTransport,
    };
    use dex_connectors_sdk::{
        Call, CallId, ConnectionRef, FailureKind, OperationRef, Query, QueryAttempt,
        StaticCredentialProvider,
    };
    use std::collections::HashMap;
    struct NoDispatch;
    impl ProviderHttpTransport for NoDispatch {
        fn send(
            &self,
            _: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            panic!("an unsigned request reached the transport")
        }
    }
    let connection = ConnectionRef::new(CONNECTOR_ID, CONNECTOR_ID);
    let credentials = StaticCredentialProvider::new(HashMap::from([(
        connection.clone(),
        scenario_credentials("valid"),
    )]));
    let transport =
        ProviderHttpClient::with_transport(Arc::new(NoDispatch), Duration::from_secs(1)).unwrap();
    let operation = connector::GetCurrentWeatherOperation::with_signing_sources(
        Arc::new(credentials),
        transport,
        ORIGIN,
        2048,
        Arc::new(SystemTime::now),
        Arc::new(|| Err(std::io::Error::other("entropy unavailable"))),
    )
    .unwrap();
    let call = Call::new(
        CallId::new("58acfd20-80ae-4aeb-8850-782757e16a28"),
        connection,
        OperationRef::new(CONNECTOR_ID, OPERATION_ID),
    )
    .unwrap();
    assert!(
        matches!(operation.invoke(&call,&QueryInput{longitude:116.3,latitude:39.9}),QueryAttempt::Retry{failure,retry_after_seconds} if failure.kind==FailureKind::Availability && retry_after_seconds==0.0)
    );
}
