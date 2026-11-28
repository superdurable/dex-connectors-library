// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

mod support;

use dex_connector_gitcode::{
    GetRepositoryInput, GetRepositoryOperation, GetRepositoryOutput, new_get_repository_step,
};
use dex_connectors_sdk::dex_sdk::{Context, Flow, HandlerResult, Step, StepDecision, StepList};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};
use std::collections::BTreeMap;
use std::error::Error;

const CONNECTION_NAME: &str = "gitcode";

// dex:group group-id:repository-lookup group-label:"Repository lookup"
// dex:explanation text:"Return the selected repository metadata."
struct ReturnRepository;

impl Step for ReturnRepository {
    type Input = QueryResult<Option<GetRepositoryOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct GetRepositoryFlow {
    query: QueryStep<GetRepositoryInput, GetRepositoryOperation>,
    complete: ReturnRepository,
}

impl GetRepositoryFlow {
    fn new(operation: GetRepositoryOperation) -> Result<Self, ValidationError> {
        let complete = ReturnRepository;
        let config = QueryStepConfig::new(
            "GetRepository",
            StepAnnotations::new(
                "repository-lookup",
                "Repository lookup",
                "Read the requested GitCode repository.",
            ),
            operation,
            ConnectionRef::new("gitcode", CONNECTION_NAME),
            |input: &GetRepositoryInput| input.clone(),
            BTreeMap::from([("fetched".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_get_repository_step(config)?,
            complete,
        })
    }
}

impl Flow for GetRepositoryFlow {
    type StartInput = GetRepositoryInput;

    fn flow_type(&self) -> &'static str {
        "RustGitCodeGetRepository"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let operation = GetRepositoryOperation::from_project(&project, CONNECTION_NAME)?;
    support::serve(GetRepositoryFlow::new(operation)?)
}
#[test]
fn retries_and_returns_repository_metadata() -> Result<(), Box<dyn Error>> {
    use dex_connectors_sdk::{SecretString, StaticCredentialProvider, providerhttp::*};
    use std::{
        collections::HashMap,
        sync::{
            Arc,
            atomic::{AtomicUsize, Ordering},
        },
        time::Duration,
    };
    struct RepositoryApi(AtomicUsize);
    impl ProviderHttpTransport for RepositoryApi {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            assert_eq!(request.method, "GET");
            assert_eq!(
                request.url,
                "https://api.gitcode.com/api/v5/repos/example/sample"
            );
            assert!(
                request
                    .headers
                    .iter()
                    .any(|(name, value)| name.eq_ignore_ascii_case("Authorization")
                        && value == "Bearer test+token")
            );
            let status = if self.0.fetch_add(1, Ordering::SeqCst) == 0 {
                503
            } else {
                200
            };
            let body=br#"{"id":123,"full_name":"example/sample","private":true,"name":"sample","owner":{"email":"excluded@example.org"}}"#;
            Ok(ProviderHttpResponse::new(
                status,
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(body)),
            ))
        }
    }
    let api = Arc::new(RepositoryApi(0.into()));
    let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
        ConnectionRef::new("gitcode", CONNECTION_NAME),
        SecretString::new("test+token"),
    )])));
    let new_flow = || {
        let http = ProviderHttpClient::with_transport(api.clone(), Duration::from_secs(10))?;
        GetRepositoryFlow::new(GetRepositoryOperation::with_http_client(
            "https://api.gitcode.com",
            credentials.clone(),
            http,
            262144,
        )?)
    };
    let result: GetRepositoryOutput = support::read_once(
        new_flow,
        GetRepositoryInput {
            owner: "example".into(),
            repo: "sample".into(),
        },
    )?;
    assert_eq!(result.full_name, "example/sample");
    assert_eq!(api.0.load(Ordering::SeqCst), 2);
    Ok(())
}
