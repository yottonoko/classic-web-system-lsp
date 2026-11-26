<%
' =============================================================================
' Sales Order & Line Item Domain Models
' =============================================================================

Class OrderLineItem
    Private m_LineNo
    Private m_Sku
    Private m_Description
    Private m_Quantity
    Private m_UnitPrice
    Private m_TaxRate
    Private m_DiscountRate

    Private Sub Class_Initialize()
        m_LineNo = 1
        m_Sku = ""
        m_Description = ""
        m_Quantity = 1
        m_UnitPrice = 0
        m_TaxRate = DEFAULT_TAX_RATE
        m_DiscountRate = 0
    End Sub

    Public Property Get LineNo()
        LineNo = m_LineNo
    End Property
    Public Property Let LineNo(val)
        m_LineNo = CLng(val)
    End Property

    Public Property Get Sku()
        Sku = m_Sku
    End Property
    Public Property Let Sku(val)
        m_Sku = UCase(Trim(CStr(val)))
    End Property

    Public Property Get Description()
        Description = m_Description
    End Property
    Public Property Let Description(val)
        m_Description = Trim(CStr(val))
    End Property

    Public Property Get Quantity()
        Quantity = m_Quantity
    End Property
    Public Property Let Quantity(val)
        m_Quantity = CLng(val)
    End Property

    Public Property Get UnitPrice()
        UnitPrice = m_UnitPrice
    End Property
    Public Property Let UnitPrice(val)
        m_UnitPrice = CDbl(val)
    End Property

    Public Property Get TaxRate()
        TaxRate = m_TaxRate
    End Property
    Public Property Let TaxRate(val)
        m_TaxRate = CDbl(val)
    End Property

    Public Property Get DiscountRate()
        DiscountRate = m_DiscountRate
    End Property
    Public Property Let DiscountRate(val)
        m_DiscountRate = CDbl(val)
    End Property

    Public Function GrossSubtotal()
        GrossSubtotal = m_Quantity * m_UnitPrice
    End Function

    Public Function DiscountAmount()
        DiscountAmount = Round(GrossSubtotal() * m_DiscountRate, 0)
    End Function

    Public Function NetSubtotal()
        NetSubtotal = GrossSubtotal() - DiscountAmount()
    End Function

    Public Function TaxAmount()
        TaxAmount = Round(NetSubtotal() * m_TaxRate, 0)
    End Function

    Public Function LineTotal()
        LineTotal = NetSubtotal() + TaxAmount()
    End Function
End Class

Class OrderModel
    Private m_OrderId
    Private m_OrderNumber
    Private m_CustomerName
    Private m_Status
    Private m_CreatedAt
    Private m_WarehouseCode
    Private m_Notes
    Private m_Lines()
    Private m_LineCount

    Private Sub Class_Initialize()
        m_OrderId = 0
        m_OrderNumber = ""
        m_CustomerName = ""
        m_Status = ORDER_STATUS_DRAFT
        m_CreatedAt = Now()
        m_WarehouseCode = WH_TOKYO
        m_Notes = ""
        m_LineCount = 0
    End Sub

    Public Property Get OrderId()
        OrderId = m_OrderId
    End Property
    Public Property Let OrderId(val)
        m_OrderId = CLng(val)
    End Property

    Public Property Get OrderNumber()
        OrderNumber = m_OrderNumber
    End Property
    Public Property Let OrderNumber(val)
        m_OrderNumber = Trim(CStr(val))
    End Property

    Public Property Get CustomerName()
        CustomerName = m_CustomerName
    End Property
    Public Property Let CustomerName(val)
        m_CustomerName = Trim(CStr(val))
    End Property

    Public Property Get Status()
        Status = m_Status
    End Property
    Public Property Let Status(val)
        m_Status = UCase(Trim(CStr(val)))
    End Property

    Public Property Get CreatedAt()
        CreatedAt = m_CreatedAt
    End Property
    Public Property Let CreatedAt(val)
        m_CreatedAt = val
    End Property

    Public Property Get WarehouseCode()
        WarehouseCode = m_WarehouseCode
    End Property
    Public Property Let WarehouseCode(val)
        m_WarehouseCode = UCase(Trim(CStr(val)))
    End Property

    Public Property Get Notes()
        Notes = m_Notes
    End Property
    Public Property Let Notes(val)
        m_Notes = CStr(val)
    End Property

    Public Property Get LineCount()
        LineCount = m_LineCount
    End Property

    Public Sub AddLine(lineItem)
        ReDim Preserve m_Lines(m_LineCount)
        Set m_Lines(m_LineCount) = lineItem
        m_LineCount = m_LineCount + 1
    End Sub

    Public Function GetLine(indexZeroBased)
        Set GetLine = m_Lines(indexZeroBased)
    End Function

    Public Function SubtotalAmount()
        Dim i, sum
        sum = 0
        For i = 0 To m_LineCount - 1
            sum = sum + m_Lines(i).NetSubtotal()
        Next
        SubtotalAmount = sum
    End Function

    Public Function TotalTaxAmount()
        Dim i, sum
        sum = 0
        For i = 0 To m_LineCount - 1
            sum = sum + m_Lines(i).TaxAmount()
        Next
        TotalTaxAmount = sum
    End Function

    Public Function ShippingFee()
        If SubtotalAmount() >= FREE_SHIPPING_THRESHOLD Or m_LineCount = 0 Then
            ShippingFee = 0
        Else
            ShippingFee = 1200
        End If
    End Function

    Public Function GrandTotal()
        GrandTotal = SubtotalAmount() + TotalTaxAmount() + ShippingFee()
    End Function

    Public Function CanTransitionTo(nextStatus)
        Dim target
        target = UCase(Trim(CStr(nextStatus)))
        If IsTerminalOrderStatus(m_Status) Then
            CanTransitionTo = False
            Exit Function
        End If
        Select Case m_Status
            Case ORDER_STATUS_DRAFT
                CanTransitionTo = (target = ORDER_STATUS_PENDING Or target = ORDER_STATUS_CANCELLED)
            Case ORDER_STATUS_PENDING
                CanTransitionTo = (target = ORDER_STATUS_APPROVED Or target = ORDER_STATUS_CANCELLED)
            Case ORDER_STATUS_APPROVED
                CanTransitionTo = (target = ORDER_STATUS_SHIPPED Or target = ORDER_STATUS_CANCELLED)
            Case ORDER_STATUS_SHIPPED
                CanTransitionTo = (target = ORDER_STATUS_COMPLETED)
            Case Else
                CanTransitionTo = False
        End Select
    End Function
End Class
%>
