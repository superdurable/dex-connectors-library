// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

mod support;

use dex_connector_personio::{
    Client, ListPersonsInput, ListPersonsOperation, PersonPage, new_list_persons_step,
};
use dex_connectors_sdk::dex_sdk::{Context, Flow, HandlerResult, Step, StepDecision, StepList};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};
use std::collections::BTreeMap;
use std::error::Error;
use std::sync::Arc;

const CONNECTION_NAME: &str = "personio";

// dex:group group-id:person-directory group-label:"Person directory"
// dex:explanation text:"Return only the selected IDs and names."
struct ReturnPersons;

impl Step for ReturnPersons {
    type Input = QueryResult<Option<PersonPage>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct ListPersonsFlow {
    query: QueryStep<ListPersonsInput, ListPersonsOperation>,
    complete: ReturnPersons,
}

impl ListPersonsFlow {
    fn new(operation: ListPersonsOperation) -> Result<Self, ValidationError> {
        let complete = ReturnPersons;
        let config = QueryStepConfig::new(
            "ListPersons",
            StepAnnotations::new(
                "person-directory",
                "Person directory",
                "Read one page of visible Personio profiles.",
            ),
            operation,
            ConnectionRef::new("personio", CONNECTION_NAME),
            |input: &ListPersonsInput| input.clone(),
            BTreeMap::from([("listed".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_persons_step(config)?,
            complete,
        })
    }
}

impl Flow for ListPersonsFlow {
    type StartInput = ListPersonsInput;

    fn flow_type(&self) -> &'static str {
        "RustPersonioListPersons"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let client = Arc::new(Client::from_project(&project, CONNECTION_NAME)?);
    support::serve(ListPersonsFlow::new(client.list_persons())?)
}
#[test]
fn refreshes_and_retries_in_dex() -> Result<(), Box<dyn Error>> {
    let fixture = support::fixture::PersonioFixture::new("ListPersons")?;
    let client = fixture.client()?;
    let result: PersonPage = support::read_once(
        || ListPersonsFlow::new(client.list_persons()),
        ListPersonsInput::default(),
    )?;
    assert_eq!(result.persons[0].id, "123");
    assert_eq!(result.next_cursor, "page2");
    fixture.verify_renewal()?;
    Ok(())
}
