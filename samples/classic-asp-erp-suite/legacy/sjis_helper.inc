<%
' =============================================================================
' レガシー Shift_JIS (CP932) 共通ユーティリティ
' =============================================================================

'' <summary>税込金額を計算して日本円表記（円）で返します。</summary>
'' <param name="subtotal">税抜金額</param>
'' <param name="taxRate">消費税率（例: 0.10）</param>
Function FormatJpyTaxIncluded(subtotal, taxRate)
    Dim total
    total = Round(CDbl(subtotal) * (1 + CDbl(taxRate)), 0)
    FormatJpyTaxIncluded = FormatNumber(total, 0, -1, 0, -1) & "円（税込）"
End Function

'' <summary>顧客区分コードを日本語名称に変換します。</summary>
Function GetCustomerRankName(rankCode)
    Select Case UCase(Trim(CStr(rankCode)))
        Case "A"
            GetCustomerRankName = "特約店（ゴールド）"
        Case "B"
            GetCustomerRankName = "一般法人"
        Case Else
            GetCustomerRankName = "新規取引先"
    End Select
End Function
%>
