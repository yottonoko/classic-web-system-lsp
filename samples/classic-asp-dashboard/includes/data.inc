<%
Class DashboardCustomer
    Public Id
    Public Name
    Public Tier
    Public Owner
    Public Email
    Public Balance
    Public Active
    Public LastInvoiceAt
    Public NextReviewAt
    Public Notes

    Public Property Get DisplayName()
        DisplayName = "#" & Id & " " & Name
    End Property

    Public Property Get TierLabel()
        Select Case LCase(Tier)
            Case "platinum"
                TierLabel = "Platinum"
            Case "gold"
                TierLabel = "Gold"
            Case "silver"
                TierLabel = "Silver"
            Case Else
                TierLabel = "Standard"
        End Select
    End Property

    Public Property Get StatusText()
        If IsOverdue Then
            StatusText = "Overdue"
        ElseIf Active Then
            StatusText = "Active"
        Else
            StatusText = "Paused"
        End If
    End Property

    Public Function IsPremium()
        IsPremium = LCase(Tier) = "gold" Or LCase(Tier) = "platinum"
    End Function

    Public Function IsOverdue()
        IsOverdue = Active And Balance > 0 And DateDiff("d", LastInvoiceAt, Date()) > 30
    End Function
End Class

Class DashboardFilter
    Public Query
    Public Tier
    Public IncludeInactive
End Class

Function NewCustomer(ByVal id, ByVal name, ByVal tier, ByVal owner, ByVal email, ByVal balance, ByVal active, ByVal lastInvoiceAt, ByVal nextReviewAt, ByVal notes)
    Dim customer
    Set customer = New DashboardCustomer

    With customer
        .Id = CStr(id)
        .Name = CStr(name)
        .Tier = LCase(CStr(tier))
        .Owner = CStr(owner)
        .Email = CStr(email)
        .Balance = CDbl(balance)
        .Active = CBool(active)
        .LastInvoiceAt = CDate(lastInvoiceAt)
        .NextReviewAt = CDate(nextReviewAt)
        .Notes = CStr(notes)
    End With

    Set NewCustomer = customer
End Function

Function BuildCustomerFixtures()
    BuildCustomerFixtures = Array( _
        NewCustomer(1001, "Northwind Textiles", "platinum", "Avery Stone", "avery@example.test", 12840.5, True, DateAdd("d", -18, Date()), DateAdd("d", 11, Date()), "Priority account with renewal questions."), _
        NewCustomer(1002, "Contoso Parts", "gold", "Morgan Lee", "morgan@example.test", 0, True, DateAdd("d", -7, Date()), DateAdd("d", 8, Date()), "Interested in the reporting add-on."), _
        NewCustomer(1003, "Fabrikam Cafe", "silver", "Harper Chen", "harper@example.test", 220.75, True, DateAdd("d", -45, Date()), DateAdd("d", 2, Date()), "Needs invoice follow-up before expansion."), _
        NewCustomer(1004, "Adventure Works", "standard", "Riley Patel", "riley@example.test", 0, False, DateAdd("d", -90, Date()), DateAdd("d", 40, Date()), "Paused during procurement review."), _
        NewCustomer(1005, "Blue Yonder Books", "gold", "Jordan Kim", "jordan@example.test", 980.0, True, DateAdd("d", -12, Date()), DateAdd("d", 19, Date()), "Requested accessibility checklist."), _
        NewCustomer(1006, "Tailspin Toys", "silver", "Quinn Rivera", "quinn@example.test", 140.0, True, DateAdd("d", -61, Date()), DateAdd("d", 5, Date()), "Seasonal campaign starts soon."), _
        NewCustomer(1007, "Litware Labs", "platinum", "Casey Nguyen", "casey@example.test", 0, True, DateAdd("d", -3, Date()), DateAdd("d", 21, Date()), "Preparing enterprise migration."), _
        NewCustomer(1008, "Proseware Studio", "standard", "Taylor Brooks", "taylor@example.test", 75.5, False, DateAdd("d", -120, Date()), DateAdd("d", 55, Date()), "Dormant but subscribed to updates.") _
    )
End Function

Function ReadDashboardFilter()
    Dim dashboardFilter
    Set dashboardFilter = New DashboardFilter

    dashboardFilter.Query = Trim(Request.QueryString("q"))
    dashboardFilter.Tier = LCase(Trim(Request.QueryString("tier")))
    dashboardFilter.IncludeInactive = ParseBoolean(Request.QueryString("inactive"))

    Set ReadDashboardFilter = dashboardFilter
End Function

Function ParseBoolean(ByVal value)
    Select Case LCase(Trim(CStr(value)))
        Case "1", "true", "yes", "on"
            ParseBoolean = True
        Case Else
            ParseBoolean = False
    End Select
End Function

Function FilterCustomers(ByVal customers, ByVal dashboardFilter)
    Dim matches()
    ReDim matches(0)

    Dim index
    Dim count
    count = -1

    For index = 0 To UBound(customers)
        If CustomerMatchesFilter(customers(index), dashboardFilter) Then
            count = count + 1
            ReDim Preserve matches(count)
            Set matches(count) = customers(index)
        End If
    Next

    If count = -1 Then
        FilterCustomers = Array()
    Else
        ReDim Preserve matches(count)
        FilterCustomers = matches
    End If
End Function

Function HasItems(ByVal values)
    On Error Resume Next

    Dim upperBound
    upperBound = UBound(values)
    HasItems = Err.Number = 0 And upperBound >= 0

    Err.Clear
    On Error GoTo 0
End Function

Function CountItems(ByVal values)
    If HasItems(values) Then
        CountItems = UBound(values) + 1
    Else
        CountItems = 0
    End If
End Function

Function CustomerMatchesFilter(ByVal customer, ByVal dashboardFilter)
    CustomerMatchesFilter = False

    If Not dashboardFilter.IncludeInactive And Not customer.Active Then
        Exit Function
    End If

    If dashboardFilter.Tier <> "" And LCase(customer.Tier) <> dashboardFilter.Tier Then
        Exit Function
    End If

    If dashboardFilter.Query <> "" Then
        Dim query
        query = LCase(dashboardFilter.Query)

        If InStr(1, LCase(customer.Name), query, vbTextCompare) = 0 _
            And InStr(1, LCase(customer.Owner), query, vbTextCompare) = 0 _
            And InStr(1, LCase(customer.Notes), query, vbTextCompare) = 0 Then
            Exit Function
        End If
    End If

    CustomerMatchesFilter = True
End Function

Function BuildMetrics(ByVal customers)
    Dim values
    Set values = Server.CreateObject("Scripting.Dictionary")

    values.Add "active", 0
    values.Add "premium", 0
    values.Add "overdue", 0
    values.Add "balance", 0

    If HasItems(customers) Then
        Dim index
        For index = 0 To UBound(customers)
            If customers(index).Active Then
                values("active") = values("active") + 1
            End If

            If customers(index).IsPremium Then
                values("premium") = values("premium") + 1
            End If

            If customers(index).IsOverdue Then
                values("overdue") = values("overdue") + 1
            End If

            values("balance") = values("balance") + customers(index).Balance
        Next
    End If

    Set BuildMetrics = values
End Function

Function FindCustomerById(ByVal customers, ByVal id)
    Set FindCustomerById = Nothing

    If Trim(CStr(id)) = "" Then
        Exit Function
    End If

    Dim index
    For index = 0 To UBound(customers)
        If CStr(customers(index).Id) = CStr(id) Then
            Set FindCustomerById = customers(index)
            Exit Function
        End If
    Next
End Function

Function FormatCurrencyValue(ByVal value)
    FormatCurrencyValue = "$" & FormatNumber(CDbl(value), 2)
End Function

Function FormatDateValue(ByVal value)
    If IsDate(value) Then
        FormatDateValue = Month(value) & "/" & Day(value) & "/" & Year(value)
    Else
        FormatDateValue = "Not scheduled"
    End If
End Function

Function TierColor(ByVal tier)
    Select Case LCase(tier)
        Case "platinum"
            TierColor = "#2f5f9f"
        Case "gold"
            TierColor = "#a46f16"
        Case "silver"
            TierColor = "#747b85"
        Case Else
            TierColor = "#236b5f"
    End Select
End Function
%>
