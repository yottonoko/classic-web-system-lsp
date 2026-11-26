<%@ Language=VBScript CodePage=65001 %>
<!--#include file="../includes/config.inc"-->
<!--#include file="../includes/formatters.inc"-->
<!--#include file="../includes/validators.inc"-->
<%
' =============================================================================
' LSP Verification Playground
' Use this page to test:
'  1. Diagnostics & Quick-Fix Code Actions (missing Option Explicit, undeclared vars)
'  2. Signature Help & Parameter Inlay Hints (multi-arg calls below)
'  3. Folding Ranges & Selection Ranges (nested If / Select Case / For / Function)
'  4. CSS Document Colors (#2563eb, #16a34a, rgb(234, 88, 12))
'  5. Cross-file Go-to-Definition, Hover, References, Rename
' =============================================================================

Function CalculateWeightedScore(basePoints, multiplier, bonusPoints, capLimit)
    Dim rawScore
    rawScore = (CDbl(basePoints) * CDbl(multiplier)) + CDbl(bonusPoints)
    If rawScore > CDbl(capLimit) Then
        CalculateWeightedScore = CDbl(capLimit)
    ElseIf rawScore < 0 Then
        CalculateWeightedScore = 0
    Else
        CalculateWeightedScore = rawScore
    End If
End Function

Sub RunPlaygroundDemo()
    Dim score, qtyErr, rangeErr, okQty, okRange
    score = CalculateWeightedScore(120, 1.5, 40, 200)
    okQty = ValidateOrderQuantity(15, 1, 100, qtyErr)
    okRange = ValidateDateRange("2026-01-01", "2026-12-31", rangeErr)

    ' Intentional undeclared variable assignment for diagnostic / quick-fix testing:
    playgroundTemporaryBanner = "Score=" & CStr(score) & " qtyOk=" & CStr(okQty) & " rangeOk=" & CStr(okRange)
    Response.Write "<div class='playground-banner'>" & playgroundTemporaryBanner & "</div>"
End Sub
%>
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Classic ASP LSP Playground</title>
    <style>
        .playground-banner {
            background-color: #2563eb;
            color: #ffffff;
            border-left: 6px solid #16a34a;
            box-shadow: 0 2px 8px rgb(234, 88, 12);
            padding: 12px 16px;
            border-radius: 6px;
        }
    </style>
</head>
<body>
    <h1>Classic ASP LSP Playground</h1>
    <% RunPlaygroundDemo %>
    <p>Formatted sample amount: <%= FormatCurrencyJpy(128500) %></p>
</body>
</html>
