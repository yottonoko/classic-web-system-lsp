<%@ Language=VBScript CodePage=65001 %>
<% Option Explicit %>
<!--#include file="../includes/config.inc"-->
<!--#include file="../includes/security.inc"-->
<!--#include file="../includes/formatters.inc"-->
<!--#include file="../includes/models/inventory_model.inc"-->
<!--#include file="../includes/models/order_model.inc"-->
<!--#include file="../includes/services/order_service.inc"-->
<!--#include file="../includes/services/billing_service.inc"-->
<!--#include file="../includes/layout/header.inc"-->
<!--#include file="../includes/layout/footer.inc"-->
<%
Dim orders, i, ord, issuedAt, dueAt, paidAmt, overdueDays, bucket, lateFee, invStatus, today
orders = BuildSampleOrders()
today = Date()

RenderPageHeader "Billing & Invoices", "billing"
%>

<section class="card">
    <h1>Accounts Receivable &amp; Invoice Aging</h1>
    <table style="width:100%; border-collapse:collapse;">
        <thead>
            <tr style="text-align:left; border-bottom:1px solid #cbd5e1;">
                <th>Invoice / Order</th>
                <th>Customer</th>
                <th>Due Date</th>
                <th>Aging Bucket</th>
                <th style="text-align:right;">Billed</th>
                <th style="text-align:right;">Paid</th>
                <th style="text-align:right;">Late Fee</th>
                <th>Status</th>
            </tr>
        </thead>
        <tbody>
            <% For i = LBound(orders) To UBound(orders) %>
                <%
                Set ord = orders(i)
                issuedAt = DateAdd("d", -20 * (i + 1), today)
                dueAt = CalculateInvoiceDueDate(issuedAt, 30)
                If ord.Status = ORDER_STATUS_COMPLETED Then
                    paidAmt = ord.GrandTotal()
                ElseIf i = 1 Then
                    paidAmt = Round(ord.GrandTotal() * 0.5, 0)
                Else
                    paidAmt = 0
                End If
                overdueDays = DaysPastDue(dueAt, today)
                bucket = ClassifyAgingBucket(overdueDays)
                lateFee = ComputeLateFee(ord.GrandTotal() - paidAmt, overdueDays, 0.06)
                invStatus = ResolveInvoiceStatus(ord.GrandTotal(), paidAmt, dueAt, today)
                %>
                <tr style="border-bottom:1px solid #e2e8f0;">
                    <td>INV-<%= ord.OrderId %> (<%= HtmlEncode(ord.OrderNumber) %>)</td>
                    <td><%= HtmlEncode(ord.CustomerName) %></td>
                    <td><%= HtmlEncode(FormatIsoDate(dueAt)) %></td>
                    <td><%= HtmlEncode(bucket) %></td>
                    <td style="text-align:right;"><%= HtmlEncode(FormatCurrencyJpy(ord.GrandTotal())) %></td>
                    <td style="text-align:right;"><%= HtmlEncode(FormatCurrencyJpy(paidAmt)) %></td>
                    <td style="text-align:right;"><%= HtmlEncode(FormatCurrencyJpy(lateFee)) %></td>
                    <td><%= RenderStatusBadge(invStatus) %></td>
                </tr>
            <% Next %>
        </tbody>
    </table>
</section>

<%
RenderPageFooter
%>
