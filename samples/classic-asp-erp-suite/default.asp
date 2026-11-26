<%@ Language=VBScript CodePage=65001 %>
<% Option Explicit %>
<!--#include file="includes/config.inc"-->
<!--#include file="includes/security.inc"-->
<!--#include file="includes/db.inc"-->
<!--#include file="includes/formatters.inc"-->
<!--#include file="includes/models/customer_model.inc"-->
<!--#include file="includes/models/inventory_model.inc"-->
<!--#include file="includes/models/order_model.inc"-->
<!--#include file="includes/services/order_service.inc"-->
<!--#include file="includes/layout/header.inc"-->
<!--#include file="includes/layout/footer.inc"-->
<%
Dim orders, inventory, totalRevenue, lowStockCount, i, ord, item
orders = BuildSampleOrders()
inventory = BuildSampleInventory()
totalRevenue = ComputeTotalOrderRevenue(orders)
lowStockCount = CountLowStockItems(inventory)

RenderPageHeader "Executive Dashboard", "dashboard"
%>

<section class="card">
    <h1>Executive ERP Dashboard</h1>
    <p>Environment: <strong><%= HtmlEncode(APP_ENV) %></strong> &middot; Active Sessions: <strong><%= CLng(Application("ActiveSessions")) %></strong></p>
    <div style="display:grid; grid-template-columns: repeat(3, 1fr); gap: 16px; margin-top: 16px;">
        <div class="card">
            <div style="color:#64748b; font-size:13px;">Active Order Revenue</div>
            <div style="font-size:24px; font-weight:700; color:#2563eb;"><%= HtmlEncode(FormatCurrencyJpy(totalRevenue)) %></div>
        </div>
        <div class="card">
            <div style="color:#64748b; font-size:13px;">Total Sample Orders</div>
            <div style="font-size:24px; font-weight:700;"><%= UBound(orders) - LBound(orders) + 1 %></div>
        </div>
        <div class="card">
            <div style="color:#64748b; font-size:13px;">Low-Stock Alerts</div>
            <div style="font-size:24px; font-weight:700; color:<% If lowStockCount > 0 Then Response.Write "#dc2626" Else Response.Write "#16a34a" %>;"><%= lowStockCount %></div>
        </div>
    </div>
</section>

<section class="card">
    <h2>Recent Sales Orders</h2>
    <table style="width:100%; border-collapse:collapse;">
        <thead>
            <tr style="text-align:left; border-bottom:1px solid #cbd5e1;">
                <th>Order #</th>
                <th>Customer</th>
                <th>Warehouse</th>
                <th>Status</th>
                <th>Created</th>
                <th style="text-align:right;">Grand Total</th>
            </tr>
        </thead>
        <tbody>
            <% For i = LBound(orders) To UBound(orders) %>
                <% Set ord = orders(i) %>
                <tr style="border-bottom:1px solid #e2e8f0;">
                    <td><a href="orders/detail.asp?id=<%= ord.OrderId %>"><%= HtmlEncode(ord.OrderNumber) %></a></td>
                    <td><%= HtmlEncode(ord.CustomerName) %></td>
                    <td><%= HtmlEncode(ord.WarehouseCode) %></td>
                    <td><%= RenderStatusBadge(ord.Status) %></td>
                    <td><%= HtmlEncode(FormatIsoDate(ord.CreatedAt)) %></td>
                    <td style="text-align:right;"><%= HtmlEncode(FormatCurrencyJpy(ord.GrandTotal())) %></td>
                </tr>
            <% Next %>
        </tbody>
    </table>
</section>

<section class="card">
    <h2>Low-Stock Reorder Queue</h2>
    <ul>
        <% For i = LBound(inventory) To UBound(inventory) %>
            <% Set item = inventory(i) %>
            <% If item.NeedsReorder() Then %>
                <li>
                    <strong><%= HtmlEncode(item.Sku) %></strong> &mdash; <%= HtmlEncode(item.ProductName) %>
                    (Available: <%= item.AvailableQty() %> / Reorder Point: <%= item.ReorderPoint %>, Warehouse: <%= HtmlEncode(item.WarehouseCode) %>)
                </li>
            <% End If %>
        <% Next %>
    </ul>
</section>

<%
RenderPageFooter
%>
