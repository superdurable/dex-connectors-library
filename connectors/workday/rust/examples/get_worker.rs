// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

mod support;

use dex_connector_workday::{
    Client, GetWorkerInput, GetWorkerOperation, WorkerSummary, new_get_worker_step,
};
use dex_connectors_sdk::dex_sdk::{Context, Flow, HandlerResult, Step, StepDecision, StepList};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};
use std::collections::BTreeMap;
use std::error::Error;
use std::sync::Arc;

const CONNECTION_NAME: &str = "workday";

// dex:group group-id:worker-lookup group-label:"Worker lookup"
// dex:explanation text:"Return only the selected IDs and names."
struct ReturnWorkers;

impl Step for ReturnWorkers {
    type Input = QueryResult<Option<WorkerSummary>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct GetWorkerFlow {
    query: QueryStep<GetWorkerInput, GetWorkerOperation>,
    complete: ReturnWorkers,
}

impl GetWorkerFlow {
    fn new(operation: GetWorkerOperation) -> Result<Self, ValidationError> {
        let complete = ReturnWorkers;
        let config = QueryStepConfig::new(
            "GetWorker",
            StepAnnotations::new(
                "worker-lookup",
                "Worker lookup",
                "Read the requested Workday profile.",
            ),
            operation,
            ConnectionRef::new("workday", CONNECTION_NAME),
            |input: &GetWorkerInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_get_worker_step(config)?,
            complete,
        })
    }
}

impl Flow for GetWorkerFlow {
    type StartInput = GetWorkerInput;

    fn flow_type(&self) -> &'static str {
        "RustWorkdayGetWorker"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let client = Arc::new(Client::from_project(&project, CONNECTION_NAME)?);
    support::serve(GetWorkerFlow::new(client.get_worker())?)
}
#[test]
fn refreshes_and_retries_in_dex() -> Result<(), Box<dyn Error>> {
    let fixture = support::fixture::WorkdayFixture::new("GetWorker")?;
    let client = fixture.client()?;
    let result: WorkerSummary = support::read_once(
        || GetWorkerFlow::new(client.get_worker()),
        GetWorkerInput {
            worker_id: "0123456789abcdef0123456789abcdef".into(),
        },
    )?;
    assert_eq!(result.id, "0123456789abcdef0123456789abcdef");
    fixture.verify_renewal()?;
    Ok(())
}
