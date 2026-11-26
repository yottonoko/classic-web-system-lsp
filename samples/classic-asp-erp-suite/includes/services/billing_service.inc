<%
' =============================================================================
' Billing & Accounts Receivable Service
' =============================================================================

Function CalculateInvoiceDueDate(issuedDate, paymentTermDays)
    CalculateInvoiceDueDate = DateAdd("d", CLng(paymentTermDays), CDate(issuedDate))
End Function

Function DaysPastDue(dueDate, referenceDate)
    Dim diff
    diff = DateDiff("d", CDate(dueDate), CDate(referenceDate))
    If diff < 0 Then
        DaysPastDue = 0
    Else
        DaysPastDue = diff
    End If
End Function

Function ClassifyAgingBucket(overdueDays)
    Dim d
    d = CLng(overdueDays)
    If d <= 0 Then
        ClassifyAgingBucket = "Current"
    ElseIf d <= 30 Then
        ClassifyAgingBucket = "1-30 Days"
    ElseIf d <= 60 Then
        ClassifyAgingBucket = "31-60 Days"
    ElseIf d <= 90 Then
        ClassifyAgingBucket = "61-90 Days"
    Else
        ClassifyAgingBucket = "90+ Days"
    End If
End Function

Function ComputeLateFee(outstandingAmount, overdueDays, annualRate)
    Dim dailyRate, fee
    If CDbl(outstandingAmount) <= 0 Or CLng(overdueDays) <= 0 Then
        ComputeLateFee = 0
        Exit Function
    End If
    dailyRate = CDbl(annualRate) / 365.0
    fee = Round(CDbl(outstandingAmount) * dailyRate * CLng(overdueDays), 0)
    ComputeLateFee = fee
End Function

Function ResolveInvoiceStatus(billedAmount, paidAmount, dueDate, referenceDate)
    Dim billed, paid
    billed = CDbl(billedAmount)
    paid = CDbl(paidAmount)
    If paid >= billed Then
        ResolveInvoiceStatus = INVOICE_STATUS_PAID
    ElseIf DaysPastDue(dueDate, referenceDate) > 0 Then
        ResolveInvoiceStatus = INVOICE_STATUS_OVERDUE
    ElseIf paid > 0 Then
        ResolveInvoiceStatus = INVOICE_STATUS_PARTIAL
    Else
        ResolveInvoiceStatus = INVOICE_STATUS_UNPAID
    End If
End Function
%>
