<%@ Language=VBScript CodePage=65001 %>
<% Option Explicit %>
<!--#include file="../includes/config.inc"-->
<!--#include file="../includes/security.inc"-->
<!--#include file="../includes/formatters.inc"-->
<!--#include file="../includes/models/inventory_model.inc"-->
<!--#include file="../includes/models/order_model.inc"-->
<!--#include file="../includes/services/order_service.inc"-->
<!--#include file="../includes/layout/header.inc"-->
<!--#include file="../includes/layout/footer.inc"-->
<%
Dim selectedWh, inventory, i, item, totalValuation
selectedWh = UCase(Trim(CStr(Request.QueryString("wh") & "")))
inventory = BuildSampleInventory()
totalValuation = 0

RenderPageHeader "Warehouse Inventory", "inventory"
%>

<section class="card">
    <h1>Warehouse Inventory &amp; Reorder Monitor</h1>
    <form method="get" action="warehouse.asp" style="display:flex; gap:10px; margin-bottom:16px;">
        <select name="wh">
            <option value="">All Warehouses</option>
            <option value="<%= WH_TOKYO %>" <% If selectedWh = WH_TOKYO Then Response.Write "selected" %>>Tokyo (<%= WH_TOKYO %>)</option>
            <option value="<%= WH_OSAKA %>" <% If selectedWh = WH_OSAKA Then Response.Write "selected" %>>Osaka (<%= WH_OSAKA %>)</option>
            <option value="<%= WH_NAGOYA %>" <% If selectedWh = WH_NAGOYA Then Response.Write "selected" %>>Nagoya (<%= WH_NAGOYA %>)</option>
            <option value="<%= WH_FUKUOKA %>" <% If selectedWh = WH_FUKUOKA Then Response.Write "selected" %>>Fukuoka (<%= WH_FUKUOKA %>)</option>
        </select>
        <button type="submit">Apply</button>
    </form>

    <table style="width:100%; border-collapse:collapse;">
        <thead>
            <tr style="text-align:left; border-bottom:1px solid #cbd5e1;">
                <th>SKU</th>
                <th>Product</th>
                <th>Warehouse</th>
                <th style="text-align:right;">On Hand</th>
                <th style="text-align:right;">Reserved</th>
                <th style="text-align:right;">Available</th>
                <th style="text-align:right;">Unit Price</th>
                <th style="text-align:right;">Margin</th>
                <th>Status</th>
            </tr>
        </thead>
        <tbody>
            <% For i = LBound(inventory) To UBound(inventory) %>
                <% Set item = inventory(i) %>
                <% If selectedWh = "" Or item.WarehouseCode = selectedWh Then %>
                    <% totalValuation = totalValuation + (item.OnHandQty * item.CostPrice) %>
                    <tr style="border-bottom:1px solid #e2e8f0;">
                        <td><code><%= HtmlEncode(item.Sku) %></code></td>
                        <td><%= HtmlEncode(item.ProductName) %></td>
                        <td><%= HtmlEncode(item.WarehouseCode) %></td>
                        <td style="text-align:right;"><%= item.OnHandQty %></td>
                        <td style="text-align:right;"><%= item.ReservedQty %></td>
                        <td style="text-align:right;"><strong><%= item.AvailableQty() %></strong></td>
                        <td style="text-align:right;"><%= HtmlEncode(FormatCurrencyJpy(item.UnitPrice)) %></td>
                        <td style="text-align:right;"><%= HtmlEncode(FormatPercentRate(item.GrossMarginRate(), 1)) %></td>
                        <td>
                            <% If item.NeedsReorder() Then %>
                                <span class="badge badge-danger">Reorder Now</span>
                            <% Else %>
                                <span class="badge badge-success">Healthy</span>
                            <% End If %>
                        </td>
                    </tr>
                <% End If %>
            <% Next %>
        </tbody>
    </table>

    <p style="margin-top:14px;">Total Cost Valuation: <strong><%= HtmlEncode(FormatCurrencyJpy(totalValuation)) %></strong></p>
</section>

<%
RenderPageFooter
%>
