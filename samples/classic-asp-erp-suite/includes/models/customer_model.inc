<%
' =============================================================================
' Customer Account Domain Model
' =============================================================================

Class CustomerAccount
    Private m_CustomerId
    Private m_CompanyName
    Private m_ContactEmail
    Private m_Tier
    Private m_CreditLimit
    Private m_CurrentBalance
    Private m_PostalCode
    Private m_Prefecture
    Private m_CityAddress
    Private m_IsActive

    Private Sub Class_Initialize()
        m_CustomerId = 0
        m_CompanyName = ""
        m_ContactEmail = ""
        m_Tier = "STANDARD"
        m_CreditLimit = 500000
        m_CurrentBalance = 0
        m_PostalCode = ""
        m_Prefecture = ""
        m_CityAddress = ""
        m_IsActive = True
    End Sub

    Public Property Get CustomerId()
        CustomerId = m_CustomerId
    End Property
    Public Property Let CustomerId(val)
        m_CustomerId = CLng(val)
    End Property

    Public Property Get CompanyName()
        CompanyName = m_CompanyName
    End Property
    Public Property Let CompanyName(val)
        m_CompanyName = Trim(CStr(val))
    End Property

    Public Property Get ContactEmail()
        ContactEmail = m_ContactEmail
    End Property
    Public Property Let ContactEmail(val)
        m_ContactEmail = Trim(CStr(val))
    End Property

    Public Property Get Tier()
        Tier = m_Tier
    End Property
    Public Property Let Tier(val)
        m_Tier = UCase(Trim(CStr(val)))
    End Property

    Public Property Get CreditLimit()
        CreditLimit = m_CreditLimit
    End Property
    Public Property Let CreditLimit(val)
        m_CreditLimit = CDbl(val)
    End Property

    Public Property Get CurrentBalance()
        CurrentBalance = m_CurrentBalance
    End Property
    Public Property Let CurrentBalance(val)
        m_CurrentBalance = CDbl(val)
    End Property

    Public Property Get IsActive()
        IsActive = m_IsActive
    End Property
    Public Property Let IsActive(val)
        m_IsActive = CBool(val)
    End Property

    Public Sub SetAddress(postal, pref, cityAddr)
        m_PostalCode = Trim(CStr(postal))
        m_Prefecture = Trim(CStr(pref))
        m_CityAddress = Trim(CStr(cityAddr))
    End Sub

    Public Function FullAddress()
        FullAddress = "〒" & m_PostalCode & " " & m_Prefecture & " " & m_CityAddress
    End Function

    Public Function TierDiscountRate()
        Select Case m_Tier
            Case "ENTERPRISE"
                TierDiscountRate = 0.12
            Case "GOLD"
                TierDiscountRate = 0.08
            Case "SILVER"
                TierDiscountRate = 0.04
            Case Else
                TierDiscountRate = 0.0
        End Select
    End Function

    Public Function AvailableCredit()
        Dim avail
        avail = m_CreditLimit - m_CurrentBalance
        If avail < 0 Then avail = 0
        AvailableCredit = avail
    End Function

    Public Function CanPurchase(orderTotal)
        If Not m_IsActive Then
            CanPurchase = False
        Else
            CanPurchase = (CDbl(orderTotal) <= AvailableCredit())
        End If
    End Function
End Class
%>
