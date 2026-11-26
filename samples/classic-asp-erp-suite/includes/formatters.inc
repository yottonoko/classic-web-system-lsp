<%
' =============================================================================
' Formatting & Presentation Helpers
' =============================================================================

Function FormatCurrencyJpy(amount)
    If Not IsNumeric(amount) Then
        FormatCurrencyJpy = "¥0"
        Exit Function
    End If
    FormatCurrencyJpy = "¥" & FormatNumber(CDbl(amount), 0, -1, 0, -1)
End Function

Function FormatCurrencyUsd(amount)
    If Not IsNumeric(amount) Then
        FormatCurrencyUsd = "$0.00"
        Exit Function
    End If
    FormatCurrencyUsd = "$" & FormatNumber(CDbl(amount), 2, -1, 0, -1)
End Function

Function FormatPercentRate(rateValue, decimals)
    If Not IsNumeric(rateValue) Then
        FormatPercentRate = "0%"
        Exit Function
    End If
    FormatPercentRate = FormatNumber(CDbl(rateValue) * 100, CInt(decimals), -1, 0, 0) & "%"
End Function

Function FormatIsoDate(dt)
    If Not IsDate(dt) Then
        FormatIsoDate = ""
        Exit Function
    End If
    FormatIsoDate = Year(dt) & "-" & Right("0" & Month(dt), 2) & "-" & Right("0" & Day(dt), 2)
End Function

Function FormatIsoDateTime(dt)
    If Not IsDate(dt) Then
        FormatIsoDateTime = ""
        Exit Function
    End If
    FormatIsoDateTime = FormatIsoDate(dt) & " " & _
                        Right("0" & Hour(dt), 2) & ":" & _
                        Right("0" & Minute(dt), 2) & ":" & _
                        Right("0" & Second(dt), 2)
End Function

Function EscapeCsvField(rawVal)
    Dim s
    s = CStr(rawVal & "")
    If InStr(s, ",") > 0 Or InStr(s, """") > 0 Or InStr(s, vbCr) > 0 Or InStr(s, vbLf) > 0 Then
        EscapeCsvField = """" & Replace(s, """", """""") & """"
    Else
        EscapeCsvField = s
    End If
End Function

Function RenderStatusBadge(statusCode)
    Dim cssClass, label
    label = GetStatusLabel(statusCode)
    Select Case UCase(Trim(CStr(statusCode)))
        Case ORDER_STATUS_COMPLETED, INVOICE_STATUS_PAID
            cssClass = "badge-success"
        Case ORDER_STATUS_APPROVED, ORDER_STATUS_SHIPPED
            cssClass = "badge-info"
        Case ORDER_STATUS_PENDING, INVOICE_STATUS_PARTIAL
            cssClass = "badge-warning"
        Case ORDER_STATUS_CANCELLED, INVOICE_STATUS_OVERDUE
            cssClass = "badge-danger"
        Case Else
            cssClass = "badge-neutral"
    End Select
    RenderStatusBadge = "<span class='badge " & cssClass & "'>" & HtmlEncode(label) & "</span>"
End Function

Function ClampPageSize(requestedSize)
    Dim n
    If Not IsNumeric(requestedSize) Then
        ClampPageSize = DEFAULT_PAGE_SIZE
        Exit Function
    End If
    n = CLng(requestedSize)
    If n < 5 Then n = 5
    If n > MAX_PAGE_SIZE Then n = MAX_PAGE_SIZE
    ClampPageSize = n
End Function

Function ComputeTotalPages(totalRows, pageSize)
    Dim rows, sz
    rows = CLng(totalRows)
    sz = ClampPageSize(pageSize)
    If rows <= 0 Then
        ComputeTotalPages = 1
    Else
        ComputeTotalPages = Int((rows + sz - 1) / sz)
    End If
End Function
%>
