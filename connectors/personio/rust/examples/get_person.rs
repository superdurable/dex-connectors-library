// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

mod support;

use dex_connector_personio::{
    Client, GetPersonInput, GetPersonOperation, PersonProfile, new_get_person_step,
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

// dex:group group-id:person-lookup group-label:"Person lookup"
// dex:explanation text:"Return only the selected IDs and names."
struct ReturnPersons;

impl Step for ReturnPersons {
    type Input = QueryResult<Option<PersonProfile>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct GetPersonFlow {
    query: QueryStep<GetPersonInput, GetPersonOperation>,
    complete: ReturnPersons,
}

impl GetPersonFlow {
    fn new(operation: GetPersonOperation) -> Result<Self, ValidationError> {
        let complete = ReturnPersons;
        let config = QueryStepConfig::new(
            "GetPerson",
            StepAnnotations::new(
                "person-lookup",
                "Person lookup",
                "Read the requested Personio profile.",
            ),
            operation,
            ConnectionRef::new("personio", CONNECTION_NAME),
            |input: &GetPersonInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_get_person_step(config)?,
            complete,
        })
    }
}

impl Flow for GetPersonFlow {
    type StartInput = GetPersonInput;

    fn flow_type(&self) -> &'static str {
        "RustPersonioGetPerson"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let client = Arc::new(Client::from_project(&project, CONNECTION_NAME)?);
    support::serve(GetPersonFlow::new(client.get_person())?)
}
#[test]
fn refreshes_and_retries_in_dex() -> Result<(), Box<dyn Error>> {
    let fixture = support::fixture::PersonioFixture::new("GetPerson")?;
    let client = fixture.client()?;
    let result: PersonProfile = support::read_once(
        || GetPersonFlow::new(client.get_person()),
        GetPersonInput {
            person_id: "123".into(),
        },
    )?;
    assert_eq!(result.id, "123");
    fixture.verify_renewal()?;
    Ok(())
}
