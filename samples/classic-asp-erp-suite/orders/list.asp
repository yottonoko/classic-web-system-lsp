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
Dim filterStatus, searchText, sortCol, sortDir, pageSize, currentPage
Dim allowedSorts, orderBySql, allOrders, i, ord, matchCount, totalPages

filterStatus = UCase(Trim(CStr(Request.QueryString("status") & "")))
searchText = LCase(Trim(CStr(Request.QueryString("q") & "")))
sortCol = Request.QueryString("sort")
sortDir = Request.QueryString("dir")
pageSize = ClampPageSize(Request.QueryString("size"))
currentPage = SafeSqlInt(Request.QueryString("page"), 1)
If currentPage < 1 Then currentPage = 1

allowedSorts = Array("order_id", "order_number", "created_at", "status")
orderBySql = BuildSafeOrderBy(sortCol, sortDir, allowedSorts, "created_at DESC")

allOrders = BuildSampleOrders()
matchCount = 0

RenderPageHeader "Sales Orders", "orders"
%>

<section class="card">
    <div style="display:flex; justify-content:space-between; align-items:center;">
        <h1>Sales Orders</h1>
        <a href="create.asp" style="background:#2563eb; color:#ffffff; padding:8px 14px; border-radius:6px; text-decoration:none;">+ New Order</a>
    </div>

    <form method="get" action="list.asp" style="display:flex; gap:12px; margin:16px 0;">
        <input type="text" name="q" value="<%= HtmlEncode(searchText) %>" placeholder="Search order # or customer..." />
        <select name="status">
            <option value="">All Statuses</option>
            <option value="<%= ORDER_STATUS_DRAFT %>" <% If filterStatus = ORDER_STATUS_DRAFT Then Response.Write "selected" %>>Draft</option>
            <option value="<%= ORDER_STATUS_PENDING %>" <% If filterStatus = ORDER_STATUS_PENDING Then Response.Write "selected" %>>Pending</option>
            <option value="<%= ORDER_STATUS_APPROVED %>" <% If filterStatus = ORDER_STATUS_APPROVED Then Response.Write "selected" %>>Approved</option>
            <option value="<%= ORDER_STATUS_SHIPPED %>" <% If filterStatus = ORDER_STATUS_SHIPPED Then Response.Write "selected" %>>Shipped</option>
            <option value="<%= ORDER_STATUS_COMPLETED %>" <% If filterStatus = ORDER_STATUS_COMPLETED Then Response.Write "selected" %>>Completed</option>
        </select>
        <button type="submit">Filter</button>
    </form>

    <p style="font-size:12px; color:#64748b;">Active SQL Sort: <code><%= HtmlEncode(orderBySql) %></code> &middot; Page Size: <%= pageSize %></p>

    <table style="width:100%; border-collapse:collapse;">
        <thead>
            <tr style="text-align:left; border-bottom:1px solid #cbd5e1;">
                <th>Order #</th>
                <th>Customer</th>
                <th>Lines</th>
                <th>Status</th>
                <th style="text-align:right;">Subtotal</th>
                <th style="text-align:right;">Tax</th>
                <th style="text-align:right;">Total</th>
            </tr>
        </thead>
        <tbody>
            <% For i = LBound(allOrders) To UBound(allOrders) %>
                <%
                Set ord = allOrders(i)
                Dim statusOk, queryOk
                statusOk = (filterStatus = "" Or ord.Status = filterStatus)
                queryOk = (searchText = "" Or InStr(LCase(ord.OrderNumber), searchText) > 0 Or InStr(LCase(ord.CustomerName), searchText) > 0)
                If statusOk And queryOk Then
                    matchCount = matchCount + 1
                %>
                <tr style="border-bottom:1px solid #e2e8f0;">
                    <td><a href="detail.asp?id=<%= ord.OrderId %>"><%= HtmlEncode(ord.OrderNumber) %></a></td>
                    <td><%= HtmlEncode(ord.CustomerName) %></td>
                    <td><%= ord.LineCount %></td>
                    <td><%= RenderStatusBadge(ord.Status) %></td>
                    <td style="text-align:right;"><%= HtmlEncode(FormatCurrencyJpy(ord.SubtotalAmount())) %></td>
                    <td style="text-align:right;"><%= HtmlEncode(FormatCurrencyJpy(ord.TotalTaxAmount())) %></td>
                    <td style="text-align:right;"><strong><%= HtmlEncode(FormatCurrencyJpy(ord.GrandTotal())) %></strong></td>
                </tr>
                <% End If %>
            <% Next %>
        </tbody>
    </table>

    <% totalPages = ComputeTotalPages(matchCount, pageSize) %>
    <p style="margin-top:12px; color:#64748b;">Showing <%= matchCount %> order(s) (Page <%= currentPage %> of <%= totalPages %>).</p>
</section>

<%
RenderPageFooter
%>
