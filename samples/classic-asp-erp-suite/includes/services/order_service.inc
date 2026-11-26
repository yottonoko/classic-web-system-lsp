<%
' =============================================================================
' Order Service (Sample In-Memory Catalog + Order Lifecycle Operations)
' =============================================================================

Function BuildSampleInventory()
    Dim items(5)

    Set items(0) = New InventoryItem
    items(0).Sku = "SRV-RACK-4U"
    items(0).ProductName = "Enterprise Rack Server 4U"
    items(0).Category = "Hardware"
    items(0).UnitPrice = 248000
    items(0).CostPrice = 175000
    items(0).ReorderPoint = 5
    items(0).OnHandQty = 14
    items(0).ReservedQty = 3
    items(0).WarehouseCode = WH_TOKYO

    Set items(1) = New InventoryItem
    items(1).Sku = "SW-CORE-48P"
    items(1).ProductName = "Managed 48-Port 10GbE Switch"
    items(1).Category = "Networking"
    items(1).UnitPrice = 89000
    items(1).CostPrice = 54000
    items(1).ReorderPoint = 8
    items(1).OnHandQty = 7
    items(1).ReservedQty = 2
    items(1).WarehouseCode = WH_TOKYO

    Set items(2) = New InventoryItem
    items(2).Sku = "UPS-3000VA"
    items(2).ProductName = "Online Sine-Wave UPS 3000VA"
    items(2).Category = "Power"
    items(2).UnitPrice = 64000
    items(2).CostPrice = 42000
    items(2).ReorderPoint = 6
    items(2).OnHandQty = 18
    items(2).ReservedQty = 1
    items(2).WarehouseCode = WH_OSAKA

    Set items(3) = New InventoryItem
    items(3).Sku = "SSD-NVME-4TB"
    items(3).ProductName = "Datacenter NVMe SSD 4TB"
    items(3).Category = "Storage"
    items(3).UnitPrice = 52000
    items(3).CostPrice = 34000
    items(3).ReorderPoint = 15
    items(3).OnHandQty = 12
    items(3).ReservedQty = 4
    items(3).WarehouseCode = WH_OSAKA

    Set items(4) = New InventoryItem
    items(4).Sku = "CBL-SFP28-3M"
    items(4).ProductName = "25G SFP28 Twinax DAC Cable 3m"
    items(4).Category = "Accessories"
    items(4).UnitPrice = 6800
    items(4).CostPrice = 3100
    items(4).ReorderPoint = 30
    items(4).OnHandQty = 96
    items(4).ReservedQty = 12
    items(4).WarehouseCode = WH_NAGOYA

    Set items(5) = New InventoryItem
    items(5).Sku = "LIC-ERP-SEAT"
    items(5).ProductName = "Annual ERP Operator License"
    items(5).Category = "Software"
    items(5).UnitPrice = 36000
    items(5).CostPrice = 4000
    items(5).ReorderPoint = 0
    items(5).OnHandQty = 999
    items(5).ReservedQty = 0
    items(5).WarehouseCode = WH_FUKUOKA

    BuildSampleInventory = items
End Function

Function FindInventoryBySku(inventoryList, skuCode)
    Dim i, target
    target = UCase(Trim(CStr(skuCode)))
    For i = LBound(inventoryList) To UBound(inventoryList)
        If inventoryList(i).Sku = target Then
            Set FindInventoryBySku = inventoryList(i)
            Exit Function
        End If
    Next
    Set FindInventoryBySku = Nothing
End Function

Function CreateOrderLine(lineNo, skuCode, descText, qty, unitPrice, discountRate)
    Dim line
    Set line = New OrderLineItem
    line.LineNo = lineNo
    line.Sku = skuCode
    line.Description = descText
    line.Quantity = qty
    line.UnitPrice = unitPrice
    line.DiscountRate = discountRate
    Set CreateOrderLine = line
End Function

Function BuildSampleOrders()
    Dim orders(3)

    Set orders(0) = New OrderModel
    orders(0).OrderId = 10041
    orders(0).OrderNumber = "SO-2026-10041"
    orders(0).CustomerName = "Kanto Logistics Corp."
    orders(0).Status = ORDER_STATUS_APPROVED
    orders(0).CreatedAt = DateAdd("d", -2, Now())
    orders(0).WarehouseCode = WH_TOKYO
    orders(0).Notes = "Priority rack deployment for Tokyo DC2."
    orders(0).AddLine CreateOrderLine(1, "SRV-RACK-4U", "Enterprise Rack Server 4U", 2, 248000, 0.08)
    orders(0).AddLine CreateOrderLine(2, "SW-CORE-48P", "Managed 48-Port 10GbE Switch", 1, 89000, 0.08)

    Set orders(1) = New OrderModel
    orders(1).OrderId = 10042
    orders(1).OrderNumber = "SO-2026-10042"
    orders(1).CustomerName = "Kansai Precision Mfg."
    orders(1).Status = ORDER_STATUS_PENDING
    orders(1).CreatedAt = DateAdd("d", -1, Now())
    orders(1).WarehouseCode = WH_OSAKA
    orders(1).Notes = "Awaiting manager credit approval."
    orders(1).AddLine CreateOrderLine(1, "UPS-3000VA", "Online Sine-Wave UPS 3000VA", 3, 64000, 0.04)
    orders(1).AddLine CreateOrderLine(2, "SSD-NVME-4TB", "Datacenter NVMe SSD 4TB", 6, 52000, 0.04)

    Set orders(2) = New OrderModel
    orders(2).OrderId = 10043
    orders(2).OrderNumber = "SO-2026-10043"
    orders(2).CustomerName = "Chubu Retail Holdings"
    orders(2).Status = ORDER_STATUS_SHIPPED
    orders(2).CreatedAt = DateAdd("d", -5, Now())
    orders(2).WarehouseCode = WH_NAGOYA
    orders(2).Notes = "Dispatched via Yamato B2B."
    orders(2).AddLine CreateOrderLine(1, "CBL-SFP28-3M", "25G SFP28 Twinax DAC Cable 3m", 20, 6800, 0.0)

    Set orders(3) = New OrderModel
    orders(3).OrderId = 10044
    orders(3).OrderNumber = "SO-2026-10044"
    orders(3).CustomerName = "Kyushu Cloud Systems"
    orders(3).Status = ORDER_STATUS_COMPLETED
    orders(3).CreatedAt = DateAdd("d", -12, Now())
    orders(3).WarehouseCode = WH_FUKUOKA
    orders(3).Notes = "Annual seat renewal."
    orders(3).AddLine CreateOrderLine(1, "LIC-ERP-SEAT", "Annual ERP Operator License", 15, 36000, 0.12)

    BuildSampleOrders = orders
End Function

Function LoadOrderById(orderId)
    Dim orders, i, targetId
    targetId = CLng(orderId)
    orders = BuildSampleOrders()
    For i = LBound(orders) To UBound(orders)
        If orders(i).OrderId = targetId Then
            Set LoadOrderById = orders(i)
            Exit Function
        End If
    Next
    Set LoadOrderById = Nothing
End Function

Function ComputeTotalOrderRevenue(orderList)
    Dim i, total
    total = 0
    For i = LBound(orderList) To UBound(orderList)
        If orderList(i).Status <> ORDER_STATUS_CANCELLED Then
            total = total + orderList(i).GrandTotal()
        End If
    Next
    ComputeTotalOrderRevenue = total
End Function

Function CountLowStockItems(inventoryList)
    Dim i, count
    count = 0
    For i = LBound(inventoryList) To UBound(inventoryList)
        If inventoryList(i).NeedsReorder() Then
            count = count + 1
        End If
    Next
    CountLowStockItems = count
End Function

Function TransitionOrderStatus(orderObj, nextStatus, actorName, ByRef errorReason)
    If Not orderObj.CanTransitionTo(nextStatus) Then
        errorReason = "Cannot transition order " & orderObj.OrderNumber & " from " & orderObj.Status & " to " & nextStatus
        TransitionOrderStatus = False
        Exit Function
    End If
    orderObj.Status = nextStatus
    WriteAuditLog "STATUS_CHANGE", "Order", orderObj.OrderId, "by=" & actorName & " -> " & nextStatus
    errorReason = ""
    TransitionOrderStatus = True
End Function
%>
