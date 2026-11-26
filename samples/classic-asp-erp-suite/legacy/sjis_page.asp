<%@ Language=VBScript CodePage=932 %>
<% Option Explicit %>
<!--#include file="sjis_helper.inc"-->
<%
Dim price, label, rankName
price = 12800
label = FormatJpyTaxIncluded(price, 0.10)
rankName = GetCustomerRankName("A")
%>
<!DOCTYPE html>
<html lang="ja">
<head>
    <meta http-equiv="Content-Type" content="text/html; charset=Shift_JIS">
    <title>レガシー Shift_JIS 請求確認</title>
</head>
<body>
    <h1>請求金額確認（Shift_JIS サンプル）</h1>
    <p>区分: <%= rankName %></p>
    <p>合計: <%= label %></p>
</body>
</html>
