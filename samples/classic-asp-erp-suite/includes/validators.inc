<%
' =============================================================================
' Input Validation Helpers
' =============================================================================

Function IsValidEmail(emailAddr)
    Dim s, atPos, dotPos
    s = Trim(CStr(emailAddr & ""))
    If Len(s) < 5 Or InStr(s, " ") > 0 Then
        IsValidEmail = False
        Exit Function
    End If
    atPos = InStr(s, "@")
    If atPos <= 1 Or atPos = Len(s) Then
        IsValidEmail = False
        Exit Function
    End If
    dotPos = InStrRev(s, ".")
    IsValidEmail = (dotPos > atPos + 1 And dotPos < Len(s))
End Function

Function IsValidSku(skuCode)
    Dim s, i, ch
    s = UCase(Trim(CStr(skuCode & "")))
    If Len(s) < 4 Or Len(s) > 24 Then
        IsValidSku = False
        Exit Function
    End If
    For i = 1 To Len(s)
        ch = Mid(s, i, 1)
        If Not ((ch >= "A" And ch <= "Z") Or (ch >= "0" And ch <= "9") Or ch = "-") Then
            IsValidSku = False
            Exit Function
        End If
    Next
    IsValidSku = True
End Function

Function IsValidJapanesePostalCode(postal)
    Dim s
    s = Replace(Trim(CStr(postal & "")), "-", "")
    IsValidJapanesePostalCode = (Len(s) = 7 And IsNumeric(s))
End Function

Function ValidateOrderQuantity(qtyValue, minQty, maxQty, ByRef errorMessage)
    Dim qty
    If Not IsNumeric(qtyValue) Then
        errorMessage = "Quantity must be numeric."
        ValidateOrderQuantity = False
        Exit Function
    End If
    qty = CLng(qtyValue)
    If qty < CLng(minQty) Then
        errorMessage = "Quantity must be at least " & CStr(minQty) & "."
        ValidateOrderQuantity = False
    ElseIf qty > CLng(maxQty) Then
        errorMessage = "Quantity cannot exceed " & CStr(maxQty) & "."
        ValidateOrderQuantity = False
    Else
        errorMessage = ""
        ValidateOrderQuantity = True
    End If
End Function

Function ValidateDateRange(startText, endText, ByRef errorMessage)
    If Not IsDate(startText) Or Not IsDate(endText) Then
        errorMessage = "Both start and end dates must be valid dates."
        ValidateDateRange = False
        Exit Function
    End If
    If CDate(startText) > CDate(endText) Then
        errorMessage = "Start date cannot be later than end date."
        ValidateDateRange = False
        Exit Function
    End If
    errorMessage = ""
    ValidateDateRange = True
End Function
%>
