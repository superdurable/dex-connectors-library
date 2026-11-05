# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Contact, ContactPage, CreateInvoiceInput, CreateInvoiceOutput, FindContactByEmailInput, FindContactByEmailOutput, GetInvoiceInput, Invoice, InvoiceContact, InvoiceLineItem, InvoiceLineItemInput, InvoicePage, InvoicePayment, ListContactsInput, ListInvoicesInput, Payment, PaymentAccount, PaymentInvoice, RecordPaymentInput, RecordPaymentOutput
from .definitions import CONNECTOR_ID, AMBIGUOUS, CREATED, CREATE_INVOICE_DEFINITION, DAILY_LIMIT_REACHED, DEFECT, FIND_CONTACT_BY_EMAIL_DEFINITION, FOUND, GET_INVOICE_DEFINITION, INVALID_RESPONSE, LISTED, LIST_CONTACTS_DEFINITION, LIST_INVOICES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, RECORDED, RECORD_PAYMENT_DEFINITION, UNCERTAIN

def list_contacts_listed(value: ContactPage, failure: Failure | None = None) -> Case[ContactPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_contacts_listed, '__module__', "dex_connectors.xero.xeromock")

def list_contacts_provider_rejected(value: ContactPage, failure: Failure | None = None) -> Case[ContactPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_contacts_provider_rejected, '__module__', "dex_connectors.xero.xeromock")

def list_contacts_daily_limit_reached(value: ContactPage, failure: Failure | None = None) -> Case[ContactPage]:
    return Case.branch_result(DAILY_LIMIT_REACHED, value, failure)

setattr(list_contacts_daily_limit_reached, '__module__', "dex_connectors.xero.xeromock")

def list_contacts_invalid_response(value: ContactPage, failure: Failure | None = None) -> Case[ContactPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_contacts_invalid_response, '__module__', "dex_connectors.xero.xeromock")

def list_contacts_defect(value: ContactPage, failure: Failure | None = None) -> Case[ContactPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_contacts_defect, '__module__', "dex_connectors.xero.xeromock")

def list_contacts_retry(failure: Failure, after_seconds: float = 0) -> Case[ContactPage]:
    return Case.retry(failure, after_seconds)

setattr(list_contacts_retry, '__module__', "dex_connectors.xero.xeromock")

def list_contacts_city_agency_contacts() -> Case[ContactPage]:
    return Case.branch_result(LISTED, ContactPage(contacts=(Contact(contact_id='025867f1-d741-4d6b-b1af-9ac774b59ba7', name='City Agency', email_address='accounts@cityagency.example.com', status='ACTIVE', is_customer=True, default_currency='USD'),), page=1, page_size=100, page_count=1, item_count=1, has_more_pages=False))

setattr(list_contacts_city_agency_contacts, '__module__', "dex_connectors.xero.xeromock")

def list_contacts_access_denied() -> Case[ContactPage]:
    return Case.branch_result(PROVIDER_REJECTED, ContactPage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listContacts", "Xero denied access to the organisation or scope."))

setattr(list_contacts_access_denied, '__module__', "dex_connectors.xero.xeromock")

def list_contacts_daily_limit() -> Case[ContactPage]:
    return Case.branch_result(DAILY_LIMIT_REACHED, ContactPage(), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "listContacts", "Xero daily API limit is exhausted."))

setattr(list_contacts_daily_limit, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_found(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_contact_by_email_found, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_not_found(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_contact_by_email_not_found, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_ambiguous(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(AMBIGUOUS, value, failure)

setattr(find_contact_by_email_ambiguous, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_provider_rejected(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_contact_by_email_provider_rejected, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_daily_limit_reached(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(DAILY_LIMIT_REACHED, value, failure)

setattr(find_contact_by_email_daily_limit_reached, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_invalid_response(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_contact_by_email_invalid_response, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_defect(value: FindContactByEmailOutput, failure: Failure | None = None) -> Case[FindContactByEmailOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_contact_by_email_defect, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_retry(failure: Failure, after_seconds: float = 0) -> Case[FindContactByEmailOutput]:
    return Case.retry(failure, after_seconds)

setattr(find_contact_by_email_retry, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_city_agency_contact() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(FOUND, FindContactByEmailOutput(email_address='accounts@cityagency.example.com', contact=Contact(contact_id='025867f1-d741-4d6b-b1af-9ac774b59ba7', name='City Agency', email_address='accounts@cityagency.example.com', status='ACTIVE', is_customer=True, default_currency='USD')))

setattr(find_contact_by_email_city_agency_contact, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_email_not_found() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(NOT_FOUND, FindContactByEmailOutput(email_address='nobody@cityagency.example.com'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "findContactByEmail", "No Xero contact has the email address."))

setattr(find_contact_by_email_email_not_found, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_email_ambiguous() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(AMBIGUOUS, FindContactByEmailOutput(email_address='shared@cityagency.example.com', matches=(Contact(contact_id='0817b764-0b4c-47e8-9cad-4ed00baf43d2', name='City Agency Shared', email_address='shared@cityagency.example.com', status='ACTIVE', is_customer=True, default_currency='USD'), Contact(contact_id='06638157-fdfa-47f4-91d0-875b5f5c18c6', name='City Agency Branch', email_address='shared@cityagency.example.com', status='ACTIVE'))), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "findContactByEmail", "Several Xero contacts have the email address."))

setattr(find_contact_by_email_email_ambiguous, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_access_denied() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(PROVIDER_REJECTED, FindContactByEmailOutput(email_address='accounts@cityagency.example.com'), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "findContactByEmail", "Xero denied access to the organisation or scope."))

setattr(find_contact_by_email_access_denied, '__module__', "dex_connectors.xero.xeromock")

def find_contact_by_email_daily_limit() -> Case[FindContactByEmailOutput]:
    return Case.branch_result(DAILY_LIMIT_REACHED, FindContactByEmailOutput(email_address='accounts@cityagency.example.com'), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "findContactByEmail", "Xero daily API limit is exhausted."))

setattr(find_contact_by_email_daily_limit, '__module__', "dex_connectors.xero.xeromock")

def get_invoice_found(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_invoice_found, '__module__', "dex_connectors.xero.xeromock")

def get_invoice_not_found(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_invoice_not_found, '__module__', "dex_connectors.xero.xeromock")

def get_invoice_provider_rejected(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_invoice_provider_rejected, '__module__', "dex_connectors.xero.xeromock")

def get_invoice_daily_limit_reached(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(DAILY_LIMIT_REACHED, value, failure)

setattr(get_invoice_daily_limit_reached, '__module__', "dex_connectors.xero.xeromock")

def get_invoice_invalid_response(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_invoice_invalid_response, '__module__', "dex_connectors.xero.xeromock")

def get_invoice_defect(value: Invoice, failure: Failure | None = None) -> Case[Invoice]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_invoice_defect, '__module__', "dex_connectors.xero.xeromock")

def get_invoice_retry(failure: Failure, after_seconds: float = 0) -> Case[Invoice]:
    return Case.retry(failure, after_seconds)

setattr(get_invoice_retry, '__module__', "dex_connectors.xero.xeromock")

def get_invoice_paid_order_invoice() -> Case[Invoice]:
    return Case.branch_result(FOUND, Invoice(invoice_id='7b3db23e-1c22-43a1-b11e-dac2e5637d70', invoice_number='INV-1042', type='ACCREC', status='PAID', contact=InvoiceContact(contact_id='025867f1-d741-4d6b-b1af-9ac774b59ba7', name='City Agency'), date='2026-10-01', due_date='2026-10-15', reference='ORDER-1042', currency_code='USD', line_items=(InvoiceLineItem(description='Spring bouquet', quantity='1', unit_amount='49.95', account_code='200', tax_type='NONE', line_amount='49.95'),), sub_total='49.95', total='49.95', amount_due='0.00', amount_paid='49.95', fully_paid_on_date='2026-10-01'))

setattr(get_invoice_paid_order_invoice, '__module__', "dex_connectors.xero.xeromock")

def get_invoice_invoice_not_found() -> Case[Invoice]:
    return Case.branch_result(NOT_FOUND, Invoice(type='', status='', contact=InvoiceContact()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getInvoice", "Xero found no such resource."))

setattr(get_invoice_invoice_not_found, '__module__', "dex_connectors.xero.xeromock")

def get_invoice_access_denied() -> Case[Invoice]:
    return Case.branch_result(PROVIDER_REJECTED, Invoice(type='', status='', contact=InvoiceContact()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getInvoice", "Xero denied access to the organisation or scope."))

setattr(get_invoice_access_denied, '__module__', "dex_connectors.xero.xeromock")

def get_invoice_daily_limit() -> Case[Invoice]:
    return Case.branch_result(DAILY_LIMIT_REACHED, Invoice(type='', status='', contact=InvoiceContact()), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "getInvoice", "Xero daily API limit is exhausted."))

setattr(get_invoice_daily_limit, '__module__', "dex_connectors.xero.xeromock")

def list_invoices_listed(value: InvoicePage, failure: Failure | None = None) -> Case[InvoicePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_invoices_listed, '__module__', "dex_connectors.xero.xeromock")

def list_invoices_provider_rejected(value: InvoicePage, failure: Failure | None = None) -> Case[InvoicePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_invoices_provider_rejected, '__module__', "dex_connectors.xero.xeromock")

def list_invoices_daily_limit_reached(value: InvoicePage, failure: Failure | None = None) -> Case[InvoicePage]:
    return Case.branch_result(DAILY_LIMIT_REACHED, value, failure)

setattr(list_invoices_daily_limit_reached, '__module__', "dex_connectors.xero.xeromock")

def list_invoices_invalid_response(value: InvoicePage, failure: Failure | None = None) -> Case[InvoicePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_invoices_invalid_response, '__module__', "dex_connectors.xero.xeromock")

def list_invoices_defect(value: InvoicePage, failure: Failure | None = None) -> Case[InvoicePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_invoices_defect, '__module__', "dex_connectors.xero.xeromock")

def list_invoices_retry(failure: Failure, after_seconds: float = 0) -> Case[InvoicePage]:
    return Case.retry(failure, after_seconds)

setattr(list_invoices_retry, '__module__', "dex_connectors.xero.xeromock")

def list_invoices_paid_order_invoices() -> Case[InvoicePage]:
    return Case.branch_result(LISTED, InvoicePage(invoices=(Invoice(invoice_id='7b3db23e-1c22-43a1-b11e-dac2e5637d70', invoice_number='INV-1042', type='ACCREC', status='PAID', contact=InvoiceContact(contact_id='025867f1-d741-4d6b-b1af-9ac774b59ba7', name='City Agency'), date='2026-10-01', due_date='2026-10-15', reference='ORDER-1042', currency_code='USD', line_items=(InvoiceLineItem(description='Spring bouquet', quantity='1', unit_amount='49.95', account_code='200', tax_type='NONE', line_amount='49.95'),), sub_total='49.95', total='49.95', amount_due='0.00', amount_paid='49.95', fully_paid_on_date='2026-10-01'),), page=1, page_size=10, page_count=1, item_count=1, has_more_pages=False))

setattr(list_invoices_paid_order_invoices, '__module__', "dex_connectors.xero.xeromock")

def list_invoices_order_without_invoice() -> Case[InvoicePage]:
    return Case.branch_result(LISTED, InvoicePage(invoices=(), page=1, page_size=10, page_count=0, item_count=0, has_more_pages=False))

setattr(list_invoices_order_without_invoice, '__module__', "dex_connectors.xero.xeromock")

def list_invoices_access_denied() -> Case[InvoicePage]:
    return Case.branch_result(PROVIDER_REJECTED, InvoicePage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listInvoices", "Xero denied access to the organisation or scope."))

setattr(list_invoices_access_denied, '__module__', "dex_connectors.xero.xeromock")

def list_invoices_daily_limit() -> Case[InvoicePage]:
    return Case.branch_result(DAILY_LIMIT_REACHED, InvoicePage(), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "listInvoices", "Xero daily API limit is exhausted."))

setattr(list_invoices_daily_limit, '__module__', "dex_connectors.xero.xeromock")

def create_invoice_created(value: CreateInvoiceOutput, failure: Failure | None = None) -> Case[CreateInvoiceOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_invoice_created, '__module__', "dex_connectors.xero.xeromock")

def create_invoice_provider_rejected(value: CreateInvoiceOutput, failure: Failure | None = None) -> Case[CreateInvoiceOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_invoice_provider_rejected, '__module__', "dex_connectors.xero.xeromock")

def create_invoice_daily_limit_reached(value: CreateInvoiceOutput, failure: Failure | None = None) -> Case[CreateInvoiceOutput]:
    return Case.branch_result(DAILY_LIMIT_REACHED, value, failure)

setattr(create_invoice_daily_limit_reached, '__module__', "dex_connectors.xero.xeromock")

def create_invoice_uncertain(value: CreateInvoiceOutput, failure: Failure) -> Case[CreateInvoiceOutput]:
    return Case.uncertain(value, failure)

setattr(create_invoice_uncertain, '__module__', "dex_connectors.xero.xeromock")

def create_invoice_defect(value: CreateInvoiceOutput, failure: Failure | None = None) -> Case[CreateInvoiceOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_invoice_defect, '__module__', "dex_connectors.xero.xeromock")

def create_invoice_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateInvoiceOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_invoice_retry, '__module__', "dex_connectors.xero.xeromock")

def create_invoice_approved_order_invoice() -> Case[CreateInvoiceOutput]:
    return Case.branch_result(CREATED, CreateInvoiceOutput(invoice=Invoice(invoice_id='7b3db23e-1c22-43a1-b11e-dac2e5637d70', invoice_number='INV-1042', type='ACCREC', status='AUTHORISED', contact=InvoiceContact(contact_id='025867f1-d741-4d6b-b1af-9ac774b59ba7', name='City Agency'), date='2026-10-01', due_date='2026-10-15', reference='ORDER-1042', currency_code='USD', line_items=(InvoiceLineItem(description='Spring bouquet', quantity='1', unit_amount='49.95', account_code='200', tax_type='NONE', line_amount='49.95'),), sub_total='49.95', total='49.95', amount_due='49.95', amount_paid='0.00'), reference='ORDER-1042'))

setattr(create_invoice_approved_order_invoice, '__module__', "dex_connectors.xero.xeromock")

def create_invoice_access_denied() -> Case[CreateInvoiceOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateInvoiceOutput(invoice=Invoice(type='', status='', contact=InvoiceContact()), reference='ORDER-1042'), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createInvoice", "Xero denied access to the organisation or scope."))

setattr(create_invoice_access_denied, '__module__', "dex_connectors.xero.xeromock")

def create_invoice_daily_limit() -> Case[CreateInvoiceOutput]:
    return Case.branch_result(DAILY_LIMIT_REACHED, CreateInvoiceOutput(invoice=Invoice(type='', status='', contact=InvoiceContact()), reference='ORDER-1042'), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "createInvoice", "Xero daily API limit is exhausted."))

setattr(create_invoice_daily_limit, '__module__', "dex_connectors.xero.xeromock")

def record_payment_recorded(value: RecordPaymentOutput, failure: Failure | None = None) -> Case[RecordPaymentOutput]:
    return Case.branch_result(RECORDED, value, failure)

setattr(record_payment_recorded, '__module__', "dex_connectors.xero.xeromock")

def record_payment_provider_rejected(value: RecordPaymentOutput, failure: Failure | None = None) -> Case[RecordPaymentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(record_payment_provider_rejected, '__module__', "dex_connectors.xero.xeromock")

def record_payment_daily_limit_reached(value: RecordPaymentOutput, failure: Failure | None = None) -> Case[RecordPaymentOutput]:
    return Case.branch_result(DAILY_LIMIT_REACHED, value, failure)

setattr(record_payment_daily_limit_reached, '__module__', "dex_connectors.xero.xeromock")

def record_payment_uncertain(value: RecordPaymentOutput, failure: Failure) -> Case[RecordPaymentOutput]:
    return Case.uncertain(value, failure)

setattr(record_payment_uncertain, '__module__', "dex_connectors.xero.xeromock")

def record_payment_defect(value: RecordPaymentOutput, failure: Failure | None = None) -> Case[RecordPaymentOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(record_payment_defect, '__module__', "dex_connectors.xero.xeromock")

def record_payment_retry(failure: Failure, after_seconds: float = 0) -> Case[RecordPaymentOutput]:
    return Case.retry(failure, after_seconds)

setattr(record_payment_retry, '__module__', "dex_connectors.xero.xeromock")

def record_payment_order_payment() -> Case[RecordPaymentOutput]:
    return Case.branch_result(RECORDED, RecordPaymentOutput(payment=Payment(payment_id='24eb9d81-f60c-493a-b54b-19095f5d4f50', date='2026-10-01', amount='49.95', status='AUTHORISED', payment_type='ACCRECPAYMENT', invoice=PaymentInvoice(invoice_id='7b3db23e-1c22-43a1-b11e-dac2e5637d70', invoice_number='INV-1042', type='ACCREC', status='PAID', amount_due='0.00', amount_paid='49.95', currency_code='USD'), account=PaymentAccount(code='090')), invoice_id='7b3db23e-1c22-43a1-b11e-dac2e5637d70'))

setattr(record_payment_order_payment, '__module__', "dex_connectors.xero.xeromock")

def record_payment_access_denied() -> Case[RecordPaymentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, RecordPaymentOutput(payment=Payment(status='', invoice=PaymentInvoice(), account=PaymentAccount()), invoice_id='7b3db23e-1c22-43a1-b11e-dac2e5637d70'), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "recordPayment", "Xero denied access to the organisation or scope."))

setattr(record_payment_access_denied, '__module__', "dex_connectors.xero.xeromock")

def record_payment_daily_limit() -> Case[RecordPaymentOutput]:
    return Case.branch_result(DAILY_LIMIT_REACHED, RecordPaymentOutput(payment=Payment(status='', invoice=PaymentInvoice(), account=PaymentAccount()), invoice_id='7b3db23e-1c22-43a1-b11e-dac2e5637d70'), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "recordPayment", "Xero daily API limit is exhausted."))

setattr(record_payment_daily_limit, '__module__', "dex_connectors.xero.xeromock")


class Mock:

    def __init__(self, connection_name: str = "xero") -> None:
        self.list_contacts_script: Query[ListContactsInput, ContactPage] = Query(LIST_CONTACTS_DEFINITION)
        self.list_contacts_script.default(list_contacts_city_agency_contacts())
        self.find_contact_by_email_script: Query[FindContactByEmailInput, FindContactByEmailOutput] = Query(FIND_CONTACT_BY_EMAIL_DEFINITION)
        self.find_contact_by_email_script.default(find_contact_by_email_city_agency_contact())
        self.get_invoice_script: Query[GetInvoiceInput, Invoice] = Query(GET_INVOICE_DEFINITION)
        self.get_invoice_script.default(get_invoice_paid_order_invoice())
        self.list_invoices_script: Query[ListInvoicesInput, InvoicePage] = Query(LIST_INVOICES_DEFINITION)
        self.list_invoices_script.default(list_invoices_paid_order_invoices())
        self.create_invoice_script: Mutation[CreateInvoiceInput, CreateInvoiceOutput] = Mutation(CREATE_INVOICE_DEFINITION)
        self.create_invoice_script.default(create_invoice_approved_order_invoice())
        self.record_payment_script: Mutation[RecordPaymentInput, RecordPaymentOutput] = Mutation(RECORD_PAYMENT_DEFINITION)
        self.record_payment_script.default(record_payment_order_payment())
        self._connection = new_connection_with_operations(
            list_contacts=self.list_contacts_script,
            find_contact_by_email=self.find_contact_by_email_script,
            get_invoice=self.get_invoice_script,
            list_invoices=self.list_invoices_script,
            create_invoice=self.create_invoice_script,
            record_payment=self.record_payment_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.xero.xeromock")

__all__ = [
    "Mock",
    "list_contacts_listed",
    "list_contacts_provider_rejected",
    "list_contacts_daily_limit_reached",
    "list_contacts_invalid_response",
    "list_contacts_defect",
    "list_contacts_retry",
    "list_contacts_city_agency_contacts",
    "list_contacts_access_denied",
    "list_contacts_daily_limit",
    "find_contact_by_email_found",
    "find_contact_by_email_not_found",
    "find_contact_by_email_ambiguous",
    "find_contact_by_email_provider_rejected",
    "find_contact_by_email_daily_limit_reached",
    "find_contact_by_email_invalid_response",
    "find_contact_by_email_defect",
    "find_contact_by_email_retry",
    "find_contact_by_email_city_agency_contact",
    "find_contact_by_email_email_not_found",
    "find_contact_by_email_email_ambiguous",
    "find_contact_by_email_access_denied",
    "find_contact_by_email_daily_limit",
    "get_invoice_found",
    "get_invoice_not_found",
    "get_invoice_provider_rejected",
    "get_invoice_daily_limit_reached",
    "get_invoice_invalid_response",
    "get_invoice_defect",
    "get_invoice_retry",
    "get_invoice_paid_order_invoice",
    "get_invoice_invoice_not_found",
    "get_invoice_access_denied",
    "get_invoice_daily_limit",
    "list_invoices_listed",
    "list_invoices_provider_rejected",
    "list_invoices_daily_limit_reached",
    "list_invoices_invalid_response",
    "list_invoices_defect",
    "list_invoices_retry",
    "list_invoices_paid_order_invoices",
    "list_invoices_order_without_invoice",
    "list_invoices_access_denied",
    "list_invoices_daily_limit",
    "create_invoice_created",
    "create_invoice_provider_rejected",
    "create_invoice_daily_limit_reached",
    "create_invoice_uncertain",
    "create_invoice_defect",
    "create_invoice_retry",
    "create_invoice_approved_order_invoice",
    "create_invoice_access_denied",
    "create_invoice_daily_limit",
    "record_payment_recorded",
    "record_payment_provider_rejected",
    "record_payment_daily_limit_reached",
    "record_payment_uncertain",
    "record_payment_defect",
    "record_payment_retry",
    "record_payment_order_payment",
    "record_payment_access_denied",
    "record_payment_daily_limit",
]
