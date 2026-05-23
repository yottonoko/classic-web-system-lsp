<%@ Language="VBScript" CodePage=65001 %>
<% Option Explicit %>
<!-- #include file="includes/layout.inc" -->
<%
Dim savedMessage
savedMessage = ""

If Request.ServerVariables("REQUEST_METHOD") = "POST" Then
    Session("sampleTheme") = Request.Form("theme")
    Session("sampleDensity") = Request.Form("density")
    savedMessage = "Settings saved for this session."
End If

Dim selectedTheme
selectedTheme = Session("sampleTheme")

If selectedTheme = "" Then
    selectedTheme = "light"
End If

Dim selectedDensity
selectedDensity = Session("sampleDensity")

If selectedDensity = "" Then
    selectedDensity = "comfortable"
End If

Function SettingChecked(ByVal actual, ByVal expected)
    If LCase(CStr(actual)) = LCase(CStr(expected)) Then
        SettingChecked = " checked"
    Else
        SettingChecked = ""
    End If
End Function
%>
<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Settings - Classic ASP Sample</title>
    <style>
        body {
            margin: 0;
            font-family: "Inter", "Segoe UI", sans-serif;
            background: #f7f5ef;
            color: #1d2733;
        }

        .page {
            max-width: 820px;
            margin: 0 auto;
            padding: 32px;
        }

        .sample-nav {
            display: flex;
            flex-wrap: wrap;
            gap: 8px;
            margin: 16px 0 28px;
        }

        .sample-nav a {
            padding: 8px 10px;
            border-radius: 6px;
            color: #1d2733;
            text-decoration: none;
        }

        .sample-nav a[aria-current="page"] {
            background: #e9f3f1;
            color: #236b5f;
            font-weight: 700;
        }

        form {
            display: grid;
            gap: 22px;
            padding: 22px;
            border: 1px solid #d8d0c2;
            border-radius: 8px;
            background: #fff;
        }

        fieldset {
            display: grid;
            gap: 12px;
            border: 0;
            margin: 0;
            padding: 0;
        }

        legend {
            font-weight: 750;
        }

        label {
            display: flex;
            align-items: center;
            gap: 10px;
        }

        button {
            width: fit-content;
            min-height: 38px;
            padding: 0 16px;
            border: 0;
            border-radius: 6px;
            background: #236b5f;
            color: #fff;
            font: inherit;
            cursor: pointer;
        }

        .message {
            padding: 12px 14px;
            border: 1px solid #9dc8b7;
            border-radius: 8px;
            background: #e9f3f1;
        }
    </style>
</head>
<body>
    <main class="page">
        <h1>Settings</h1>
        <%= RenderSampleNavigation("settings") %>

        <% If savedMessage <> "" Then %>
            <p class="message"><%= Server.HTMLEncode(savedMessage) %></p>
        <% End If %>

        <form method="post" action="settings.asp">
            <fieldset>
                <legend>Theme</legend>
                <label>
                    <input type="radio" name="theme" value="light" <%= SettingChecked(selectedTheme, "light") %>>
                    Light
                </label>
                <label>
                    <input type="radio" name="theme" value="contrast" <%= SettingChecked(selectedTheme, "contrast") %>>
                    High contrast
                </label>
            </fieldset>

            <fieldset>
                <legend>Density</legend>
                <label>
                    <input type="radio" name="density" value="comfortable" <%= SettingChecked(selectedDensity, "comfortable") %>>
                    Comfortable
                </label>
                <label>
                    <input type="radio" name="density" value="compact" <%= SettingChecked(selectedDensity, "compact") %>>
                    Compact
                </label>
            </fieldset>

            <button type="submit">Save settings</button>
        </form>
    </main>
</body>
</html>
