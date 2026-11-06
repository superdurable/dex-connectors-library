# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CreateCustomerInput, CreateCustomerOutput, CreateInvoiceInput, CreateInvoiceOutput, Customer, FindCustomerInput, FindCustomerOutput, GetInvoiceInput, Invoice, InvoiceLine, InvoiceLineInput, InvoicePage, ListInvoicesInput, Payment, PaymentApplication, RecordPaymentInput, SendInvoiceInput
from .definitions import CONNECTOR_ID, AMBIGUOUS, CREATED, CREATE_CUSTOMER_DEFINITION, CREATE_INVOICE_DEFINITION, DEFECT, FIND_CUSTOMER_DEFINITION, FOUND, GET_INVOICE_DEFINITION, INVALID_RESPONSE, LISTED, LIST_INVOICES_DEFINITION, NAME_CONFLICT, NOT_FOUND, PROVIDER_REJECTED, RECORDED, RECORD_PAYMENT_DEFINITION, SEND_INVOICE_DEFINITION, SENT, UNCERTAIN

def find_customer_found(value: FindCustomerOutput, failure: Failure | None = None) -> Case[FindCustomerOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_customer_found, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def find_customer_not_found(value: FindCustomerOutput, failure: Failure | None = None) -> Case[FindCustomerOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_customer_not_found, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def find_customer_ambiguous(value: FindCustomerOutput, failure: Failure | None = None) -> Case[FindCustomerOutput]:
    return Case.branch_result(AMBIGUOUS, value, failure)

setattr(find_customer_ambiguous, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def find_customer_provider_rejected(value: FindCustomerOutput, failure: Failure | None = None) -> Case[FindCustomerOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_customer_provider_rejected, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def find_customer_invalid_response(value: FindCustomerOutput, failure: Failure | None = None) -> Case[FindCustomerOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_customer_invalid_response, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def find_customer_defect(value: FindCustomerOutput, failure: Failure | None = None) -> Case[FindCustomerOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_customer_defect, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def find_customer_retry(failure: Failure, after_seconds: float = 0) -> Case[FindCustomerOutput]:
    return Case.retry(failure, after_seconds)

setattr(find_customer_retry, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def find_customer_existing_city_agency() -> Case[FindCustomerOutput]:
    return Case.branch_result(FOUND, FindCustomerOutput(email_address='accounts@cityagency.example.com', customer=Customer(customer_id='58', display_name='City Agency', email_address='accounts@cityagency.example.com', is_active=True)))

setattr(find_customer_existing_city_agency, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def find_customer_not_found_failure() -> Case[FindCustomerOutput]:
    return Case.branch_result(NOT_FOUND, FindCustomerOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "findCustomer", "No customer matches; inactive customers count only when the input includes them."))

setattr(find_customer_not_found_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def find_customer_shared_email() -> Case[FindCustomerOutput]:
    return Case.branch_result(AMBIGUOUS, FindCustomerOutput(email_address='shared@example.com', matches=(Customer(customer_id='61', display_name='City Agency East', email_address='shared@example.com', is_active=True), Customer(customer_id='62', display_name='City Agency West', email_address='shared@example.com', is_active=True))))

setattr(find_customer_shared_email, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def find_customer_provider_rejected_failure() -> Case[FindCustomerOutput]:
    return Case.branch_result(PROVIDER_REJECTED, FindCustomerOutput(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "findCustomer", "QuickBooks authorization must be renewed before looking up the customer."))

setattr(find_customer_provider_rejected_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def find_customer_invalid_response_failure() -> Case[FindCustomerOutput]:
    return Case.branch_result(INVALID_RESPONSE, FindCustomerOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "findCustomer", "QuickBooks returned an invalid or oversized customer list."))

setattr(find_customer_invalid_response_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_customer_created(value: CreateCustomerOutput, failure: Failure | None = None) -> Case[CreateCustomerOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_customer_created, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_customer_name_conflict(value: CreateCustomerOutput, failure: Failure | None = None) -> Case[CreateCustomerOutput]:
    return Case.branch_result(NAME_CONFLICT, value, failure)

setattr(create_customer_name_conflict, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_customer_provider_rejected(value: CreateCustomerOutput, failure: Failure | None = None) -> Case[CreateCustomerOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_customer_provider_rejected, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_customer_uncertain(value: CreateCustomerOutput, failure: Failure) -> Case[CreateCustomerOutput]:
    return Case.uncertain(value, failure)

setattr(create_customer_uncertain, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_customer_defect(value: CreateCustomerOutput, failure: Failure | None = None) -> Case[CreateCustomerOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_customer_defect, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_customer_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateCustomerOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_customer_retry, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_customer_new_buyer_customer() -> Case[CreateCustomerOutput]:
    return Case.branch_result(CREATED, CreateCustomerOutput(customer=Customer(customer_id='59', display_name='New Buyer', email_address='newbuyer@example.com', is_active=True), display_name='New Buyer'))

setattr(create_customer_new_buyer_customer, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_customer_name_conflict_failure() -> Case[CreateCustomerOutput]:
    return Case.branch_result(NAME_CONFLICT, CreateCustomerOutput(customer=Customer()), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "createCustomer", "QuickBooks already uses this display name."))

setattr(create_customer_name_conflict_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_customer_provider_rejected_failure() -> Case[CreateCustomerOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateCustomerOutput(customer=Customer()), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "createCustomer", "QuickBooks authorization must be renewed before creating the customer; nothing was created."))

setattr(create_customer_provider_rejected_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_invoice_created(value: CreateInvoiceOutput, failure: Failure | None = None) -> Case[CreateInvoiceOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_invoice_created, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_invoice_provider_rejected(value: CreateInvoiceOutput, failure: Failure | None = None) -> Case[CreateInvoiceOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_invoice_provider_rejected, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_invoice_uncertain(value: CreateInvoiceOutput, failure: Failure) -> Case[CreateInvoiceOutput]:
    return Case.uncertain(value, failure)

setattr(create_invoice_uncertain, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_invoice_defect(value: CreateInvoiceOutput, failure: Failure | None = None) -> Case[CreateInvoiceOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_invoice_defect, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_invoice_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateInvoiceOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_invoice_retry, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_invoice_new_buyer_order_invoice() -> Case[CreateInvoiceOutput]:
    return Case.branch_result(CREATED, CreateInvoiceOutput(invoice=Invoice(invoice_id='131', doc_number='ORDER-2048', customer_id='59', lines=(InvoiceLine(detail_type='SalesItemLineDetail', amount='75.00', item_id='1', quantity='1', unit_price='75.00'),), total_amount='75.00', balance='75.00', email_status='NotSet', bill_email='newbuyer@example.com'), doc_number='ORDER-2048'))

setattr(create_invoice_new_buyer_order_invoice, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def create_invoice_provider_rejected_failure() -> Case[CreateInvoiceOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateInvoiceOutput(invoice=Invoice()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "createInvoice", "QuickBooks found no such invoice item; nothing was created."))

setattr(create_invoice_provider_rejected_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def get_invoice_found(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_invoice_found, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def get_invoice_not_found(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_invoice_not_found, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def get_invoice_provider_rejected(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_invoice_provider_rejected, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def get_invoice_invalid_response(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_invoice_invalid_response, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def get_invoice_defect(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_invoice_defect, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def get_invoice_retry(failure: Failure, after_seconds: float = 0) -> Case[Invoice]:
    return Case.retry(failure, after_seconds)

setattr(get_invoice_retry, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def get_invoice_paid_city_agency_invoice() -> Case[Invoice]:
    return Case.branch_result(FOUND, Invoice(invoice_id='130', doc_number='ORDER-1042', customer_id='58', lines=(InvoiceLine(detail_type='SalesItemLineDetail', amount='99.90', item_id='1', quantity='2', unit_price='49.95'), InvoiceLine(detail_type='SalesItemLineDetail', amount='12.50', item_id='2', quantity='1', unit_price='12.5')), total_amount='112.40', balance='0', email_status='EmailSent', bill_email='accounts@cityagency.example.com', linked_payment_ids=('186',)))

setattr(get_invoice_paid_city_agency_invoice, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def get_invoice_not_found_failure() -> Case[Invoice]:
    return Case.branch_result(NOT_FOUND, Invoice(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getInvoice", "The invoice does not exist in the company or was deleted."))

setattr(get_invoice_not_found_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def get_invoice_provider_rejected_failure() -> Case[Invoice]:
    return Case.branch_result(PROVIDER_REJECTED, Invoice(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getInvoice", "QuickBooks authorization must be renewed before reading the invoice."))

setattr(get_invoice_provider_rejected_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def get_invoice_invalid_response_failure() -> Case[Invoice]:
    return Case.branch_result(INVALID_RESPONSE, Invoice(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getInvoice", "QuickBooks returned an invalid or oversized invoice."))

setattr(get_invoice_invalid_response_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def list_invoices_listed(value: InvoicePage, failure: Failure | None = None) -> Case[InvoicePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_invoices_listed, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def list_invoices_provider_rejected(value: InvoicePage, failure: Failure | None = None) -> Case[InvoicePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_invoices_provider_rejected, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def list_invoices_invalid_response(value: InvoicePage, failure: Failure | None = None) -> Case[InvoicePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_invoices_invalid_response, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def list_invoices_defect(value: InvoicePage, failure: Failure | None = None) -> Case[InvoicePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_invoices_defect, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def list_invoices_retry(failure: Failure, after_seconds: float = 0) -> Case[InvoicePage]:
    return Case.retry(failure, after_seconds)

setattr(list_invoices_retry, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def list_invoices_city_agency_open_invoice() -> Case[InvoicePage]:
    return Case.branch_result(LISTED, InvoicePage(invoices=(Invoice(invoice_id='130', doc_number='ORDER-1042', customer_id='58', lines=(InvoiceLine(detail_type='SalesItemLineDetail', amount='99.90', item_id='1', quantity='2', unit_price='49.95'), InvoiceLine(detail_type='SalesItemLineDetail', amount='12.50', item_id='2', quantity='1', unit_price='12.5')), total_amount='112.40', balance='112.40', email_status='NotSet', bill_email='accounts@cityagency.example.com'),), start_position=1, max_results=10, has_more_pages=False))

setattr(list_invoices_city_agency_open_invoice, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def list_invoices_provider_rejected_failure() -> Case[InvoicePage]:
    return Case.branch_result(PROVIDER_REJECTED, InvoicePage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listInvoices", "QuickBooks authorization must be renewed before listing invoices."))

setattr(list_invoices_provider_rejected_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def list_invoices_invalid_response_failure() -> Case[InvoicePage]:
    return Case.branch_result(INVALID_RESPONSE, InvoicePage(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listInvoices", "QuickBooks returned an invalid or oversized page of invoices."))

setattr(list_invoices_invalid_response_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def send_invoice_sent(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(SENT, value, failure)

setattr(send_invoice_sent, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def send_invoice_provider_rejected(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(send_invoice_provider_rejected, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def send_invoice_uncertain(value: Invoice, failure: Failure) -> Case[Invoice]:
    return Case.uncertain(value, failure)

setattr(send_invoice_uncertain, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def send_invoice_defect(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_invoice_defect, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def send_invoice_retry(failure: Failure, after_seconds: float = 0) -> Case[Invoice]:
    return Case.retry(failure, after_seconds)

setattr(send_invoice_retry, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def send_invoice_city_agency_invoice_delivery() -> Case[Invoice]:
    return Case.branch_result(SENT, Invoice(invoice_id='130', doc_number='ORDER-1042', customer_id='58', lines=(InvoiceLine(detail_type='SalesItemLineDetail', amount='99.90', item_id='1', quantity='2', unit_price='49.95'), InvoiceLine(detail_type='SalesItemLineDetail', amount='12.50', item_id='2', quantity='1', unit_price='12.5')), total_amount='112.40', balance='0', email_status='EmailSent', bill_email='accounts@cityagency.example.com', linked_payment_ids=('186',)))

setattr(send_invoice_city_agency_invoice_delivery, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def send_invoice_provider_rejected_failure() -> Case[Invoice]:
    return Case.branch_result(PROVIDER_REJECTED, Invoice(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "sendInvoice", "QuickBooks rejected delivery because invoice 132 has no recipient address; nothing was sent."))

setattr(send_invoice_provider_rejected_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def record_payment_recorded(value: Payment, failure: Failure | None = None) -> Case[Payment]:
    return Case.branch_result(RECORDED, value, failure)

setattr(record_payment_recorded, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def record_payment_provider_rejected(value: Payment, failure: Failure | None = None) -> Case[Payment]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(record_payment_provider_rejected, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def record_payment_uncertain(value: Payment, failure: Failure) -> Case[Payment]:
    return Case.uncertain(value, failure)

setattr(record_payment_uncertain, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def record_payment_defect(value: Payment, failure: Failure | None = None) -> Case[Payment]:
    return Case.branch_result(DEFECT, value, failure)

setattr(record_payment_defect, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def record_payment_retry(failure: Failure, after_seconds: float = 0) -> Case[Payment]:
    return Case.retry(failure, after_seconds)

setattr(record_payment_retry, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def record_payment_city_agency_order_payment() -> Case[Payment]:
    return Case.branch_result(RECORDED, Payment(payment_id='186', customer_id='58', total_amount='112.40', applied_invoices=(PaymentApplication(invoice_id='130', amount='112.40'),)))

setattr(record_payment_city_agency_order_payment, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

def record_payment_provider_rejected_failure() -> Case[Payment]:
    return Case.branch_result(PROVIDER_REJECTED, Payment(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "recordPayment", "QuickBooks rejected payment against another customer's invoice; nothing was recorded."))

setattr(record_payment_provider_rejected_failure, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")


class Mock:

    def __init__(self, connection_name: str = "quickbooks") -> None:
        self.find_customer_script: Query[FindCustomerInput, FindCustomerOutput] = Query(FIND_CUSTOMER_DEFINITION)
        self.find_customer_script.default(find_customer_existing_city_agency())
        self.create_customer_script: Mutation[CreateCustomerInput, CreateCustomerOutput] = Mutation(CREATE_CUSTOMER_DEFINITION)
        self.create_customer_script.default(create_customer_new_buyer_customer())
        self.create_invoice_script: Mutation[CreateInvoiceInput, CreateInvoiceOutput] = Mutation(CREATE_INVOICE_DEFINITION)
        self.create_invoice_script.default(create_invoice_new_buyer_order_invoice())
        self.get_invoice_script: Query[GetInvoiceInput, Invoice] = Query(GET_INVOICE_DEFINITION)
        self.get_invoice_script.default(get_invoice_paid_city_agency_invoice())
        self.list_invoices_script: Query[ListInvoicesInput, InvoicePage] = Query(LIST_INVOICES_DEFINITION)
        self.list_invoices_script.default(list_invoices_city_agency_open_invoice())
        self.send_invoice_script: Mutation[SendInvoiceInput, Invoice] = Mutation(SEND_INVOICE_DEFINITION)
        self.send_invoice_script.default(send_invoice_city_agency_invoice_delivery())
        self.record_payment_script: Mutation[RecordPaymentInput, Payment] = Mutation(RECORD_PAYMENT_DEFINITION)
        self.record_payment_script.default(record_payment_city_agency_order_payment())
        self._connection = new_connection_with_operations(
            find_customer=self.find_customer_script,
            create_customer=self.create_customer_script,
            create_invoice=self.create_invoice_script,
            get_invoice=self.get_invoice_script,
            list_invoices=self.list_invoices_script,
            send_invoice=self.send_invoice_script,
            record_payment=self.record_payment_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.intuit_quickbooks.quickbooksmock")

__all__ = [
    "Mock",
    "find_customer_found",
    "find_customer_not_found",
    "find_customer_ambiguous",
    "find_customer_provider_rejected",
    "find_customer_invalid_response",
    "find_customer_defect",
    "find_customer_retry",
    "find_customer_existing_city_agency",
    "find_customer_not_found_failure",
    "find_customer_shared_email",
    "find_customer_provider_rejected_failure",
    "find_customer_invalid_response_failure",
    "create_customer_created",
    "create_customer_name_conflict",
    "create_customer_provider_rejected",
    "create_customer_uncertain",
    "create_customer_defect",
    "create_customer_retry",
    "create_customer_new_buyer_customer",
    "create_customer_name_conflict_failure",
    "create_customer_provider_rejected_failure",
    "create_invoice_created",
    "create_invoice_provider_rejected",
    "create_invoice_uncertain",
    "create_invoice_defect",
    "create_invoice_retry",
    "create_invoice_new_buyer_order_invoice",
    "create_invoice_provider_rejected_failure",
    "get_invoice_found",
    "get_invoice_not_found",
    "get_invoice_provider_rejected",
    "get_invoice_invalid_response",
    "get_invoice_defect",
    "get_invoice_retry",
    "get_invoice_paid_city_agency_invoice",
    "get_invoice_not_found_failure",
    "get_invoice_provider_rejected_failure",
    "get_invoice_invalid_response_failure",
    "list_invoices_listed",
    "list_invoices_provider_rejected",
    "list_invoices_invalid_response",
    "list_invoices_defect",
    "list_invoices_retry",
    "list_invoices_city_agency_open_invoice",
    "list_invoices_provider_rejected_failure",
    "list_invoices_invalid_response_failure",
    "send_invoice_sent",
    "send_invoice_provider_rejected",
    "send_invoice_uncertain",
    "send_invoice_defect",
    "send_invoice_retry",
    "send_invoice_city_agency_invoice_delivery",
    "send_invoice_provider_rejected_failure",
    "record_payment_recorded",
    "record_payment_provider_rejected",
    "record_payment_uncertain",
    "record_payment_defect",
    "record_payment_retry",
    "record_payment_city_agency_order_payment",
    "record_payment_provider_rejected_failure",
]
