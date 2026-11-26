<%
' =============================================================================
' Inventory & Warehouse Stock Domain Models
' =============================================================================

Class InventoryItem
    Private m_Sku
    Private m_ProductName
    Private m_Category
    Private m_UnitPrice
    Private m_CostPrice
    Private m_TaxRate
    Private m_ReorderPoint
    Private m_OnHandQty
    Private m_ReservedQty
    Private m_WarehouseCode

    Private Sub Class_Initialize()
        m_Sku = ""
        m_ProductName = ""
        m_Category = "General"
        m_UnitPrice = 0
        m_CostPrice = 0
        m_TaxRate = DEFAULT_TAX_RATE
        m_ReorderPoint = 10
        m_OnHandQty = 0
        m_ReservedQty = 0
        m_WarehouseCode = WH_TOKYO
    End Sub

    Public Property Get Sku()
        Sku = m_Sku
    End Property
    Public Property Let Sku(val)
        m_Sku = UCase(Trim(CStr(val)))
    End Property

    Public Property Get ProductName()
        ProductName = m_ProductName
    End Property
    Public Property Let ProductName(val)
        m_ProductName = Trim(CStr(val))
    End Property

    Public Property Get Category()
        Category = m_Category
    End Property
    Public Property Let Category(val)
        m_Category = Trim(CStr(val))
    End Property

    Public Property Get UnitPrice()
        UnitPrice = m_UnitPrice
    End Property
    Public Property Let UnitPrice(val)
        m_UnitPrice = CDbl(val)
    End Property

    Public Property Get CostPrice()
        CostPrice = m_CostPrice
    End Property
    Public Property Let CostPrice(val)
        m_CostPrice = CDbl(val)
    End Property

    Public Property Get TaxRate()
        TaxRate = m_TaxRate
    End Property
    Public Property Let TaxRate(val)
        m_TaxRate = CDbl(val)
    End Property

    Public Property Get ReorderPoint()
        ReorderPoint = m_ReorderPoint
    End Property
    Public Property Let ReorderPoint(val)
        m_ReorderPoint = CLng(val)
    End Property

    Public Property Get OnHandQty()
        OnHandQty = m_OnHandQty
    End Property
    Public Property Let OnHandQty(val)
        m_OnHandQty = CLng(val)
    End Property

    Public Property Get ReservedQty()
        ReservedQty = m_ReservedQty
    End Property
    Public Property Let ReservedQty(val)
        m_ReservedQty = CLng(val)
    End Property

    Public Property Get WarehouseCode()
        WarehouseCode = m_WarehouseCode
    End Property
    Public Property Let WarehouseCode(val)
        m_WarehouseCode = UCase(Trim(CStr(val)))
    End Property

    Public Function AvailableQty()
        Dim avail
        avail = m_OnHandQty - m_ReservedQty
        If avail < 0 Then avail = 0
        AvailableQty = avail
    End Function

    Public Function NeedsReorder()
        NeedsReorder = (AvailableQty() <= m_ReorderPoint)
    End Function

    Public Function GrossMarginRate()
        If m_UnitPrice <= 0 Then
            GrossMarginRate = 0
        Else
            GrossMarginRate = (m_UnitPrice - m_CostPrice) / m_UnitPrice
        End If
    End Function

    Public Function ReserveStock(requestedQty)
        Dim qty
        qty = CLng(requestedQty)
        If qty <= 0 Or qty > AvailableQty() Then
            ReserveStock = False
        Else
            m_ReservedQty = m_ReservedQty + qty
            ReserveStock = True
        End If
    End Function
End Class
%>
