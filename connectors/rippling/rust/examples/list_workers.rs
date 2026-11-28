// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

mod support;

use dex_connector_rippling::{
    Client, ListWorkersInput, ListWorkersOperation, WorkerPage, new_list_workers_step,
};
use dex_connectors_sdk::dex_sdk::{Context, Flow, HandlerResult, Step, StepDecision, StepList};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};
use std::collections::BTreeMap;
use std::error::Error;
use std::sync::Arc;

const CONNECTION_NAME: &str = "rippling";

// dex:group group-id:worker-directory group-label:"Worker directory"
// dex:explanation text:"Return only the selected IDs and names."
struct ReturnWorkers;

impl Step for ReturnWorkers {
    type Input = QueryResult<Option<WorkerPage>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct ListWorkersFlow {
    query: QueryStep<ListWorkersInput, ListWorkersOperation>,
    complete: ReturnWorkers,
}

impl ListWorkersFlow {
    fn new(operation: ListWorkersOperation) -> Result<Self, ValidationError> {
        let complete = ReturnWorkers;
        let config = QueryStepConfig::new(
            "ListWorkers",
            StepAnnotations::new(
                "worker-directory",
                "Worker directory",
                "Read one page of visible Rippling profiles.",
            ),
            operation,
            ConnectionRef::new("rippling", CONNECTION_NAME),
            |input: &ListWorkersInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_workers_step(config)?,
            complete,
        })
    }
}

impl Flow for ListWorkersFlow {
    type StartInput = ListWorkersInput;

    fn flow_type(&self) -> &'static str {
        "RustRipplingListWorkers"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let client = Arc::new(Client::from_project(&project, CONNECTION_NAME)?);
    support::serve(ListWorkersFlow::new(client.list_workers())?)
}
#[test]
fn retries_in_dex() -> Result<(), Box<dyn Error>> {
    let fixture = support::fixture::RipplingFixture::new("ListWorkers");
    let client = fixture.client()?;
    let result: WorkerPage = support::read_once(
        || ListWorkersFlow::new(client.list_workers()),
        ListWorkersInput {
            limit: 2,
            cursor: "a+=&".into(),
        },
    )?;
    assert_eq!(result.workers[0].id, "worker-123");
    assert_eq!(result.next_cursor, "later+/=");
    assert_eq!(result.workers[0].title, "Engineer");
    fixture.verify_reads();
    Ok(())
}
