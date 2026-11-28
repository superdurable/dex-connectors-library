// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

mod support;

use dex_connector_sage_intacct::{
    Client, InvoiceList, ListInvoicesInput, ListInvoicesOperation, new_list_invoices_step,
};
use dex_connectors_sdk::dex_sdk::{Context, Flow, HandlerResult, Step, StepDecision, StepList};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};
use std::collections::BTreeMap;
use std::error::Error;
use std::sync::Arc;

const CONNECTION_NAME: &str = "sage-intacct";

// dex:group group-id:invoice-directory group-label:"Invoices"
// dex:explanation text:"Return only the invoice keys and IDs."
struct ReturnInvoices;

impl Step for ReturnInvoices {
    type Input = QueryResult<Option<InvoiceList>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct ListInvoicesFlow {
    query: QueryStep<ListInvoicesInput, ListInvoicesOperation>,
    complete: ReturnInvoices,
}

impl ListInvoicesFlow {
    fn new(operation: ListInvoicesOperation) -> Result<Self, ValidationError> {
        let complete = ReturnInvoices;
        let config = QueryStepConfig::new(
            "ListInvoices",
            StepAnnotations::new(
                "invoice-directory",
                "Invoices",
                "Read one page of accounts receivable invoices.",
            ),
            operation,
            ConnectionRef::new("sage-intacct", CONNECTION_NAME),
            |input: &ListInvoicesInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_invoices_step(config)?,
            complete,
        })
    }
}

impl Flow for ListInvoicesFlow {
    type StartInput = ListInvoicesInput;

    fn flow_type(&self) -> &'static str {
        "RustSageIntacctListInvoices"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let client = Arc::new(Client::from_project(&project, CONNECTION_NAME)?);
    support::serve(ListInvoicesFlow::new(client.list_invoices())?)
}
#[test]
fn refreshes_and_retries_in_dex() -> Result<(), Box<dyn Error>> {
    let fixture = support::fixture::SageIntacctFixture::new("ListInvoices")?;
    let client = fixture.client()?;
    let result: InvoiceList = support::read_once(
        || ListInvoicesFlow::new(client.list_invoices()),
        ListInvoicesInput::default(),
    )?;
    assert_eq!(result.invoices[0].key, "0123456789abcdef0123456789abcdef");
    assert_eq!(result.next, "/page/2");
    fixture.verify_renewal()?;
    Ok(())
}
