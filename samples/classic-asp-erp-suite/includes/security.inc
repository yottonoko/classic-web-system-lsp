<%
' =============================================================================
' Security, Authentication, RBAC, Escaping & Audit Logging Helpers
' =============================================================================

Function HtmlEncode(rawText)
    Dim s
    If IsNull(rawText) Or IsEmpty(rawText) Then
        HtmlEncode = ""
        Exit Function
    End If
    s = CStr(rawText)
    s = Replace(s, "&", "&amp;")
    s = Replace(s, "<", "&lt;")
    s = Replace(s, ">", "&gt;")
    s = Replace(s, """", "&quot;")
    s = Replace(s, "'", "&#39;")
    HtmlEncode = s
End Function

Function JsEncode(rawText)
    Dim s
    If IsNull(rawText) Or IsEmpty(rawText) Then
        JsEncode = ""
        Exit Function
    End If
    s = CStr(rawText)
    s = Replace(s, "\", "\\")
    s = Replace(s, "'", "\'")
    s = Replace(s, """", "\""")
    s = Replace(s, vbCrLf, "\n")
    s = Replace(s, vbCr, "\r")
    s = Replace(s, vbLf, "\n")
    JsEncode = s
End Function

Function GetCurrentUserId()
    Dim uid
    uid = Session("UserId")
    If IsNumeric(uid) Then
        GetCurrentUserId = CLng(uid)
    Else
        GetCurrentUserId = 0
    End If
End Function

Function GetCurrentUserName()
    Dim name
    name = CStr(Session("UserName") & "")
    If Len(Trim(name)) = 0 Then
        GetCurrentUserName = "guest"
    Else
        GetCurrentUserName = name
    End If
End Function

Function GetCurrentUserRole()
    Dim role
    role = LCase(Trim(CStr(Session("UserRole") & "")))
    If role = "" Then
        GetCurrentUserRole = ROLE_VIEWER
    Else
        GetCurrentUserRole = role
    End If
End Function

Function RoleRank(roleName)
    Select Case LCase(Trim(CStr(roleName)))
        Case ROLE_ADMIN
            RoleRank = 40
        Case ROLE_MANAGER
            RoleRank = 30
        Case ROLE_OPERATOR
            RoleRank = 20
        Case ROLE_VIEWER
            RoleRank = 10
        Case Else
            RoleRank = 0
    End Select
End Function

Function HasPermission(requiredRole)
    HasPermission = (RoleRank(GetCurrentUserRole()) >= RoleRank(requiredRole))
End Function

Sub RequireRole(requiredRole)
    If Not HasPermission(requiredRole) Then
        Response.Status = "403 Forbidden"
        Response.Write "<div class='alert alert-danger'>Access denied. Required role: " & HtmlEncode(requiredRole) & "</div>"
        Response.End
    End If
End Sub

Function EnsureCsrfToken()
    Dim token, i, chars, idx
    token = CStr(Session("CsrfToken") & "")
    If Len(token) >= 24 Then
        EnsureCsrfToken = token
        Exit Function
    End If
    chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
    Randomize Timer
    token = ""
    For i = 1 To 32
        idx = Int(Rnd() * Len(chars)) + 1
        token = token & Mid(chars, idx, 1)
    Next
    Session("CsrfToken") = token
    EnsureCsrfToken = token
End Function

Function VerifyCsrfToken(submittedToken)
    Dim expected
    expected = CStr(Session("CsrfToken") & "")
    If Len(expected) < 24 Then
        VerifyCsrfToken = False
        Exit Function
    End If
    VerifyCsrfToken = (CStr(submittedToken & "") = expected)
End Function

Sub WriteAuditLog(actionName, entityType, entityId, detailText)
    Dim entry
    entry = Now() & " | user=" & GetCurrentUserName() & _
            " | action=" & actionName & _
            " | entity=" & entityType & "#" & CStr(entityId) & _
            " | detail=" & detailText
    Application.Lock
    Application("LastAuditEntry") = entry
    Application("TotalRequests") = CLng(Application("TotalRequests")) + 1
    Application.Unlock
End Sub
%>
