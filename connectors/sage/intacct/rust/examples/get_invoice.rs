// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

mod support;

use dex_connector_sage_intacct::{
    Client, GetInvoiceInput, GetInvoiceOperation, InvoiceSummary, new_get_invoice_step,
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

// dex:group group-id:invoice-lookup group-label:"Invoice lookup"
// dex:explanation text:"Return only the invoice keys and IDs."
struct ReturnInvoices;

impl Step for ReturnInvoices {
    type Input = QueryResult<Option<InvoiceSummary>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct GetInvoiceFlow {
    query: QueryStep<GetInvoiceInput, GetInvoiceOperation>,
    complete: ReturnInvoices,
}

impl GetInvoiceFlow {
    fn new(operation: GetInvoiceOperation) -> Result<Self, ValidationError> {
        let complete = ReturnInvoices;
        let config = QueryStepConfig::new(
            "GetInvoice",
            StepAnnotations::new(
                "invoice-lookup",
                "Invoice lookup",
                "Read the requested Sage Intacct invoice.",
            ),
            operation,
            ConnectionRef::new("sage-intacct", CONNECTION_NAME),
            |input: &GetInvoiceInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_get_invoice_step(config)?,
            complete,
        })
    }
}

impl Flow for GetInvoiceFlow {
    type StartInput = GetInvoiceInput;

    fn flow_type(&self) -> &'static str {
        "RustSageIntacctGetInvoice"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let client = Arc::new(Client::from_project(&project, CONNECTION_NAME)?);
    support::serve(GetInvoiceFlow::new(client.get_invoice())?)
}
#[test]
fn refreshes_and_retries_in_dex() -> Result<(), Box<dyn Error>> {
    let fixture = support::fixture::SageIntacctFixture::new("GetInvoice")?;
    let client = fixture.client()?;
    let result: InvoiceSummary = support::read_once(
        || GetInvoiceFlow::new(client.get_invoice()),
        GetInvoiceInput {
            invoice_key: "0123456789abcdef0123456789abcdef".into(),
        },
    )?;
    assert_eq!(result.key, "0123456789abcdef0123456789abcdef");
    fixture.verify_renewal()?;
    Ok(())
}
