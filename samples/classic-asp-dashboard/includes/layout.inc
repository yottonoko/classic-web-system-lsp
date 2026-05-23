<%
Function RenderSampleNavigation(ByVal currentPage)
    Dim items
    items = Array( _
        Array("dashboard", "default.asp", "Dashboard"), _
        Array("customers", "customers.asp", "Customers"), _
        Array("reports", "reports.asp", "Reports"), _
        Array("settings", "settings.asp", "Settings") _
    )

    Dim html
    html = "<nav class=""sample-nav"" aria-label=""Sample pages"">"

    Dim index
    For index = 0 To UBound(items)
        html = html & "<a href=""" & items(index)(1) & """" & CurrentPageAttribute(currentPage, items(index)(0)) & ">" & Server.HTMLEncode(items(index)(2)) & "</a>"
    Next

    RenderSampleNavigation = html & "</nav>"
End Function

Function CurrentPageAttribute(ByVal currentPage, ByVal itemPage)
    If LCase(CStr(currentPage)) = LCase(CStr(itemPage)) Then
        CurrentPageAttribute = " aria-current=""page"""
    Else
        CurrentPageAttribute = ""
    End If
End Function

Function RenderTierOptions(ByVal selectedTier)
    Dim tiers
    tiers = Array( _
        Array("", "All tiers"), _
        Array("platinum", "Platinum"), _
        Array("gold", "Gold"), _
        Array("silver", "Silver"), _
        Array("standard", "Standard") _
    )

    Dim html
    html = ""

    Dim index
    For index = 0 To UBound(tiers)
        html = html & "<option value=""" & Server.HTMLEncode(tiers(index)(0)) & """" & SelectedAttribute(selectedTier, tiers(index)(0)) & ">" & Server.HTMLEncode(tiers(index)(1)) & "</option>"
    Next

    RenderTierOptions = html
End Function

Function SelectedAttribute(ByVal actual, ByVal expected)
    If LCase(CStr(actual)) = LCase(CStr(expected)) Then
        SelectedAttribute = " selected"
    Else
        SelectedAttribute = ""
    End If
End Function

Function CheckedAttribute(ByVal checked)
    If checked Then
        CheckedAttribute = " checked"
    Else
        CheckedAttribute = ""
    End If
End Function
%>
