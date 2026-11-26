<%@ Language=VBScript CodePage=65001 %>
<% Option Explicit %>
<!--#include file="../includes/config.inc"-->
<!--#include file="../includes/security.inc"-->
<!--#include file="../includes/db.inc"-->
<!--#include file="../includes/formatters.inc"-->
<!--#include file="../includes/models/inventory_model.inc"-->
<!--#include file="../includes/models/order_model.inc"-->
<!--#include file="../includes/services/order_service.inc"-->
<!--#include file="../includes/layout/header.inc"-->
<!--#include file="../includes/layout/footer.inc"-->
<%
Dim orderId, ord, actionMsg, errReason, nextStatus, csrfToken, i, line
orderId = SafeSqlInt(Request.QueryString("id"), 10041)
Set ord = LoadOrderById(orderId)
csrfToken = EnsureCsrfToken()
actionMsg = ""
errReason = ""

If Request.ServerVariables("REQUEST_METHOD") = "POST" And Not (ord Is Nothing) Then
    If Not VerifyCsrfToken(Request.Form("csrf_token")) Then
        errReason = "Invalid CSRF token."
    Else
        nextStatus = UCase(Trim(CStr(Request.Form("next_status") & "")))
        If TransitionOrderStatus(ord, nextStatus, GetCurrentUserName(), errReason) Then
            actionMsg = "Order transitioned to " & GetStatusLabel(ord.Status) & "."
        End If
    End If
End If

RenderPageHeader "Order Detail", "orders"
%>

<% If ord Is Nothing Then %>
    <section class="card">
        <h1>Order Not Found</h1>
        <p>No order matched ID <code><%= orderId %></code>.</p>
        <a href="list.asp">&larr; Back to Orders</a>
    </section>
<% Else %>
    <section class="card">
        <h1><%= HtmlEncode(ord.OrderNumber) %> &mdash; <%= RenderStatusBadge(ord.Status) %></h1>
        <p>Customer: <strong><%= HtmlEncode(ord.CustomerName) %></strong> &middot; Warehouse: <strong><%= HtmlEncode(ord.WarehouseCode) %></strong> &middot; Created: <%= HtmlEncode(FormatIsoDateTime(ord.CreatedAt)) %></p>
        <p>Notes: <%= HtmlEncode(ord.Notes) %></p>

        <% If Len(actionMsg) > 0 Then %>
            <div style="padding:10px; background:#dcfce7; color:#166534; border-radius:6px; margin-bottom:12px;"><%= HtmlEncode(actionMsg) %></div>
        <% End If %>
        <% If Len(errReason) > 0 Then %>
            <div style="padding:10px; background:#fee2e2; color:#991b1b; border-radius:6px; margin-bottom:12px;"><%= HtmlEncode(errReason) %></div>
        <% End If %>

        <table style="width:100%; border-collapse:collapse; margin-top:16px;">
            <thead>
                <tr style="text-align:left; border-bottom:1px solid #cbd5e1;">
                    <th>#</th>
                    <th>SKU</th>
                    <th>Description</th>
                    <th style="text-align:right;">Qty</th>
                    <th style="text-align:right;">Unit Price</th>
                    <th style="text-align:right;">Discount</th>
                    <th style="text-align:right;">Tax</th>
                    <th style="text-align:right;">Line Total</th>
                </tr>
            </thead>
            <tbody>
                <% For i = 0 To ord.LineCount - 1 %>
                    <% Set line = ord.GetLine(i) %>
                    <tr style="border-bottom:1px solid #e2e8f0;">
                        <td><%= line.LineNo %></td>
                        <td><code><%= HtmlEncode(line.Sku) %></code></td>
                        <td><%= HtmlEncode(line.Description) %></td>
                        <td style="text-align:right;"><%= line.Quantity %></td>
                        <td style="text-align:right;"><%= HtmlEncode(FormatCurrencyJpy(line.UnitPrice)) %></td>
                        <td style="text-align:right;"><%= HtmlEncode(FormatPercentRate(line.DiscountRate, 1)) %></td>
                        <td style="text-align:right;"><%= HtmlEncode(FormatCurrencyJpy(line.TaxAmount())) %></td>
                        <td style="text-align:right;"><strong><%= HtmlEncode(FormatCurrencyJpy(line.LineTotal())) %></strong></td>
                    </tr>
                <% Next %>
            </tbody>
        </table>

        <div style="margin-top:16px; text-align:right;">
            <div>Subtotal: <strong><%= HtmlEncode(FormatCurrencyJpy(ord.SubtotalAmount())) %></strong></div>
            <div>Tax: <strong><%= HtmlEncode(FormatCurrencyJpy(ord.TotalTaxAmount())) %></strong></div>
            <div>Shipping: <strong><%= HtmlEncode(FormatCurrencyJpy(ord.ShippingFee())) %></strong></div>
            <div style="font-size:18px; margin-top:6px;">Grand Total: <strong><%= HtmlEncode(FormatCurrencyJpy(ord.GrandTotal())) %></strong></div>
        </div>

        <% If Not IsTerminalOrderStatus(ord.Status) Then %>
            <form method="post" action="detail.asp?id=<%= ord.OrderId %>" style="margin-top:18px; display:flex; gap:10px;">
                <input type="hidden" name="csrf_token" value="<%= HtmlEncode(csrfToken) %>" />
                <select name="next_status">
                    <option value="<%= ORDER_STATUS_APPROVED %>">Approve</option>
                    <option value="<%= ORDER_STATUS_SHIPPED %>">Mark Shipped</option>
                    <option value="<%= ORDER_STATUS_COMPLETED %>">Complete</option>
                    <option value="<%= ORDER_STATUS_CANCELLED %>">Cancel</option>
                </select>
                <button type="submit" data-confirm="Apply this status transition?">Update Status</button>
            </form>
        <% End If %>
    </section>
<% End If %>

<%
RenderPageFooter
%>
