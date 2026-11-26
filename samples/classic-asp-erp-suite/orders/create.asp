<%@ Language=VBScript CodePage=65001 %>
<% Option Explicit %>
<!--#include file="../includes/config.inc"-->
<!--#include file="../includes/security.inc"-->
<!--#include file="../includes/formatters.inc"-->
<!--#include file="../includes/validators.inc"-->
<!--#include file="../includes/models/customer_model.inc"-->
<!--#include file="../includes/models/inventory_model.inc"-->
<!--#include file="../includes/models/order_model.inc"-->
<!--#include file="../includes/services/order_service.inc"-->
<!--#include file="../includes/layout/header.inc"-->
<!--#include file="../includes/layout/footer.inc"-->
<%
Dim csrfToken, inventory, errText, createdOrder, customer
Dim customerName, contactEmail, skuCode, qtyInput, qtyErr, invItem, newLine

csrfToken = EnsureCsrfToken()
inventory = BuildSampleInventory()
errText = ""
Set createdOrder = Nothing

If Request.ServerVariables("REQUEST_METHOD") = "POST" Then
    customerName = Trim(CStr(Request.Form("customer_name") & ""))
    contactEmail = Trim(CStr(Request.Form("contact_email") & ""))
    skuCode = UCase(Trim(CStr(Request.Form("sku") & "")))
    qtyInput = Trim(CStr(Request.Form("quantity") & ""))

    If Not VerifyCsrfToken(Request.Form("csrf_token")) Then
        errText = "Session CSRF verification failed."
    ElseIf Len(customerName) < 2 Then
        errText = "Customer name is required."
    ElseIf Not IsValidEmail(contactEmail) Then
        errText = "A valid contact email address is required."
    ElseIf Not IsValidSku(skuCode) Then
        errText = "Invalid SKU format."
    ElseIf Not ValidateOrderQuantity(qtyInput, 1, 500, qtyErr) Then
        errText = qtyErr
    Else
        Set invItem = FindInventoryBySku(inventory, skuCode)
        If invItem Is Nothing Then
            errText = "SKU not found in catalog: " & skuCode
        ElseIf Not invItem.ReserveStock(CLng(qtyInput)) Then
            errText = "Insufficient available stock for " & invItem.Sku & " (Available: " & invItem.AvailableQty() & ")."
        Else
            Set customer = New CustomerAccount
            customer.CustomerId = 9001
            customer.CompanyName = customerName
            customer.ContactEmail = contactEmail
            customer.Tier = "GOLD"
            customer.CreditLimit = 2000000
            customer.SetAddress "100-0001", "Tokyo", "Chiyoda-ku 1-1-1"

            Set createdOrder = New OrderModel
            createdOrder.OrderId = 10099
            createdOrder.OrderNumber = "SO-2026-10099"
            createdOrder.CustomerName = customer.CompanyName
            createdOrder.Status = ORDER_STATUS_PENDING
            createdOrder.WarehouseCode = invItem.WarehouseCode
            Set newLine = CreateOrderLine(1, invItem.Sku, invItem.ProductName, CLng(qtyInput), invItem.UnitPrice, customer.TierDiscountRate())
            createdOrder.AddLine newLine

            If Not customer.CanPurchase(createdOrder.GrandTotal()) Then
                errText = "Order exceeds customer's available credit (" & FormatCurrencyJpy(customer.AvailableCredit()) & ")."
                Set createdOrder = Nothing
            Else
                WriteAuditLog "ORDER_CREATE", "Order", createdOrder.OrderId, "sku=" & invItem.Sku & " qty=" & qtyInput
            End If
        End If
    End If
End If

RenderPageHeader "Create Order", "orders"
%>

<section class="card">
    <h1>Create New Sales Order</h1>
    <% If Len(errText) > 0 Then %>
        <div style="padding:10px; background:#fee2e2; color:#991b1b; border-radius:6px; margin-bottom:14px;"><%= HtmlEncode(errText) %></div>
    <% End If %>

    <% If Not (createdOrder Is Nothing) Then %>
        <div style="padding:12px; background:#dcfce7; color:#166534; border-radius:6px; margin-bottom:14px;">
            Created <strong><%= HtmlEncode(createdOrder.OrderNumber) %></strong> for <%= HtmlEncode(createdOrder.CustomerName) %>
            &mdash; Grand Total: <strong><%= HtmlEncode(FormatCurrencyJpy(createdOrder.GrandTotal())) %></strong>
        </div>
    <% End If %>

    <form method="post" action="create.asp" style="display:grid; gap:12px; max-width:520px;">
        <input type="hidden" name="csrf_token" value="<%= HtmlEncode(csrfToken) %>" />
        <label>Customer Name <input type="text" name="customer_name" value="<%= HtmlEncode(customerName) %>" required /></label>
        <label>Contact Email <input type="email" name="contact_email" value="<%= HtmlEncode(contactEmail) %>" required /></label>
        <label>SKU
            <select name="sku">
                <option value="SRV-RACK-4U">SRV-RACK-4U — Enterprise Rack Server 4U</option>
                <option value="SW-CORE-48P">SW-CORE-48P — Managed 48-Port 10GbE Switch</option>
                <option value="UPS-3000VA">UPS-3000VA — Online Sine-Wave UPS 3000VA</option>
                <option value="SSD-NVME-4TB">SSD-NVME-4TB — Datacenter NVMe SSD 4TB</option>
            </select>
        </label>
        <label>Quantity <input type="number" name="quantity" min="1" max="500" value="2" /></label>
        <button type="submit">Submit Order</button>
    </form>
</section>

<%
RenderPageFooter
%>
