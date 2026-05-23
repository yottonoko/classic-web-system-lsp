<%@ Language="VBScript" CodePage=65001 %>
<% Option Explicit %>
<!-- #include file="includes/data.inc" -->
<!-- #include file="includes/layout.inc" -->
<%
Dim filter
Set filter = ReadDashboardFilter()

Dim customers
customers = BuildCustomerFixtures()

Dim filteredCustomers
filteredCustomers = FilterCustomers(customers, filter)

Dim metrics
Set metrics = BuildMetrics(filteredCustomers)

Dim selectedCustomerId
selectedCustomerId = Request.QueryString("customer")

Dim selectedCustomer
Set selectedCustomer = FindCustomerById(customers, selectedCustomerId)

Dim notification
notification = BuildNotificationMessage(filter, CountItems(filteredCustomers))

Sub RenderMetricCards(ByVal metricMap)
    Dim keys
    keys = Array("active", "premium", "overdue", "balance")

    Dim key
    For Each key In keys
        Response.Write RenderMetricCard(key, metricMap(key))
    Next
End Sub

Function RenderMetricCard(ByVal metricKey, ByVal metricValue)
    Dim label
    Dim hint

    Select Case metricKey
        Case "active"
            label = "Active customers"
            hint = "Included in the current filter"
        Case "premium"
            label = "Premium accounts"
            hint = "Gold or platinum tier"
        Case "overdue"
            label = "Overdue invoices"
            hint = "Needs follow-up"
        Case Else
            label = "Open balance"
            hint = "Formatted with a helper"
            metricValue = FormatCurrencyValue(metricValue)
    End Select

    RenderMetricCard = _
        "<article class=""metric metric-" & Server.HTMLEncode(metricKey) & """>" & _
            "<span class=""metric-label"">" & Server.HTMLEncode(label) & "</span>" & _
            "<strong>" & Server.HTMLEncode(CStr(metricValue)) & "</strong>" & _
            "<small>" & Server.HTMLEncode(hint) & "</small>" & _
        "</article>"
End Function

Function RenderCustomerRows(ByVal customerList, ByVal activeCustomerId)
    Dim html
    html = ""

    If HasItems(customerList) Then
        Dim index
        For index = 0 To UBound(customerList)
            html = html & RenderCustomerRow(customerList(index), activeCustomerId)
        Next
    End If

    If html = "" Then
        html = "<tr><td colspan=""6"" class=""empty"">No customers match the current filter.</td></tr>"
    End If

    RenderCustomerRows = html
End Function

Function RenderCustomerRow(ByVal customer, ByVal activeCustomerId)
    Dim rowClass
    rowClass = "customer-row"

    If CStr(customer.Id) = CStr(activeCustomerId) Then
        rowClass = rowClass & " is-selected"
    End If

    If customer.IsOverdue Then
        rowClass = rowClass & " is-overdue"
    End If

    RenderCustomerRow = _
        "<tr class=""" & rowClass & """ data-tier=""" & Server.HTMLEncode(customer.Tier) & """>" & _
            "<td><a href=""?customer=" & Server.URLEncode(customer.Id) & """>" & Server.HTMLEncode(customer.DisplayName) & "</a></td>" & _
            "<td>" & Server.HTMLEncode(customer.TierLabel) & "</td>" & _
            "<td>" & Server.HTMLEncode(customer.Owner) & "</td>" & _
            "<td>" & Server.HTMLEncode(FormatCurrencyValue(customer.Balance)) & "</td>" & _
            "<td>" & Server.HTMLEncode(FormatDateValue(customer.NextReviewAt)) & "</td>" & _
            "<td><span class=""status " & StatusClass(customer) & """>" & Server.HTMLEncode(customer.StatusText) & "</span></td>" & _
        "</tr>"
End Function

Function StatusClass(ByVal customer)
    If customer.IsOverdue Then
        StatusClass = "status-overdue"
    ElseIf customer.Active Then
        StatusClass = "status-active"
    Else
        StatusClass = "status-paused"
    End If
End Function

Function RenderCustomerDetail(ByVal customer)
    If customer Is Nothing Then
        RenderCustomerDetail = _
            "<section class=""detail-panel empty-detail"">" & _
                "<h2>No customer selected</h2>" & _
                "<p>Select a customer row to inspect hover, references, and linked HTML/CSS/JS behavior.</p>" & _
            "</section>"
        Exit Function
    End If

    Dim badgeStyle
    badgeStyle = "background-color: " & TierColor(customer.Tier) & ";"

    RenderCustomerDetail = _
        "<section class=""detail-panel"" aria-labelledby=""detail-title"">" & _
            "<div class=""detail-heading"">" & _
                "<h2 id=""detail-title"">" & Server.HTMLEncode(customer.DisplayName) & "</h2>" & _
                "<span class=""tier-badge"" style=""" & badgeStyle & """>" & Server.HTMLEncode(customer.TierLabel) & "</span>" & _
            "</div>" & _
            "<dl>" & _
                "<dt>Account owner</dt><dd>" & Server.HTMLEncode(customer.Owner) & "</dd>" & _
                "<dt>Email</dt><dd><a href=""mailto:" & Server.HTMLEncode(customer.Email) & """>" & Server.HTMLEncode(customer.Email) & "</a></dd>" & _
                "<dt>Last invoice</dt><dd>" & Server.HTMLEncode(FormatDateValue(customer.LastInvoiceAt)) & "</dd>" & _
                "<dt>Next review</dt><dd>" & Server.HTMLEncode(FormatDateValue(customer.NextReviewAt)) & "</dd>" & _
                "<dt>Notes</dt><dd>" & Server.HTMLEncode(customer.Notes) & "</dd>" & _
            "</dl>" & _
        "</section>"
End Function

Function BuildNotificationMessage(ByVal dashboardFilter, ByVal visibleCount)
    Dim message
    message = visibleCount & " customer records"

    If dashboardFilter.Tier <> "" Then
        message = message & " in " & dashboardFilter.Tier & " tier"
    End If

    If dashboardFilter.Query <> "" Then
        message = message & " matching """ & dashboardFilter.Query & """"
    End If

    If dashboardFilter.IncludeInactive Then
        message = message & ", including paused accounts"
    End If

    BuildNotificationMessage = message
End Function
%>
<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Classic ASP Customer Dashboard</title>
    <style>
        :root {
            color-scheme: light;
            font-family: "Inter", "Segoe UI", sans-serif;
            --page: #f6f4ef;
            --ink: #1d2733;
            --muted: #68727f;
            --line: #d8d0c2;
            --accent: #236b5f;
            --danger: #b33a3a;
            --gold: #a46f16;
            --blue: #2f5f9f;
        }

        * {
            box-sizing: border-box;
        }

        body {
            margin: 0;
            min-height: 100vh;
            background: var(--page);
            color: var(--ink);
        }

        .page-shell {
            display: grid;
            grid-template-columns: minmax(220px, 300px) minmax(0, 1fr);
            min-height: 100vh;
        }

        .sidebar {
            padding: 28px;
            border-right: 1px solid var(--line);
            background: #ffffff;
        }

        .sidebar h1 {
            margin: 0 0 8px;
            font-size: 1.5rem;
            letter-spacing: 0;
        }

        .sidebar p {
            margin: 0 0 24px;
            color: var(--muted);
            line-height: 1.5;
        }

        .filter-stack {
            display: grid;
            gap: 16px;
        }

        .sample-nav {
            display: grid;
            gap: 8px;
            margin: 0 0 24px;
        }

        .sample-nav a {
            padding: 8px 10px;
            border-radius: 6px;
            color: var(--ink);
            text-decoration: none;
        }

        .sample-nav a[aria-current="page"] {
            background: #e9f3f1;
            color: var(--accent);
            font-weight: 700;
        }

        .field {
            display: grid;
            gap: 6px;
        }

        .field label,
        .check-field {
            font-weight: 650;
        }

        input,
        select,
        button {
            width: 100%;
            min-height: 38px;
            border: 1px solid var(--line);
            border-radius: 6px;
            font: inherit;
        }

        input,
        select {
            padding: 8px 10px;
            background: #fff;
        }

        .check-field {
            display: flex;
            align-items: center;
            gap: 10px;
        }

        .check-field input {
            width: 18px;
            min-height: 18px;
        }

        button {
            border-color: transparent;
            background: var(--accent);
            color: #ffffff;
            cursor: pointer;
        }

        .content {
            padding: 30px;
        }

        .notice {
            display: flex;
            justify-content: space-between;
            gap: 16px;
            align-items: center;
            margin-bottom: 24px;
            padding: 14px 16px;
            border: 1px solid var(--line);
            border-radius: 8px;
            background: #fffaf1;
        }

        .notice strong {
            color: var(--accent);
        }

        .metric-grid {
            display: grid;
            grid-template-columns: repeat(4, minmax(130px, 1fr));
            gap: 14px;
            margin-bottom: 24px;
        }

        .metric {
            display: grid;
            gap: 8px;
            padding: 16px;
            border: 1px solid var(--line);
            border-radius: 8px;
            background: #ffffff;
        }

        .metric strong {
            font-size: 1.75rem;
        }

        .metric small,
        .metric-label {
            color: var(--muted);
        }

        .workspace {
            display: grid;
            grid-template-columns: minmax(0, 1.4fr) minmax(260px, 0.8fr);
            gap: 18px;
            align-items: start;
        }

        .table-panel,
        .detail-panel {
            border: 1px solid var(--line);
            border-radius: 8px;
            background: #ffffff;
            overflow: hidden;
        }

        table {
            width: 100%;
            border-collapse: collapse;
        }

        th,
        td {
            padding: 12px 14px;
            border-bottom: 1px solid var(--line);
            text-align: left;
            vertical-align: top;
        }

        th {
            color: var(--muted);
            font-size: 0.82rem;
            text-transform: uppercase;
        }

        tr:last-child td {
            border-bottom: 0;
        }

        .customer-row.is-selected {
            background: #e9f3f1;
        }

        .customer-row.is-overdue td:first-child a {
            color: var(--danger);
        }

        .status {
            display: inline-flex;
            min-width: 76px;
            justify-content: center;
            padding: 4px 8px;
            border-radius: 999px;
            font-size: 0.82rem;
            font-weight: 700;
        }

        .status-active {
            background: #ddefe7;
            color: #245f45;
        }

        .status-paused {
            background: #ece7df;
            color: #6b5d4c;
        }

        .status-overdue {
            background: #f8dede;
            color: var(--danger);
        }

        .detail-panel {
            padding: 18px;
        }

        .detail-heading {
            display: flex;
            justify-content: space-between;
            gap: 12px;
            align-items: center;
            margin-bottom: 12px;
        }

        .detail-heading h2 {
            margin: 0;
            font-size: 1.25rem;
        }

        .tier-badge {
            border-radius: 999px;
            color: #ffffff;
            font-weight: 700;
            padding: 5px 10px;
        }

        dl {
            display: grid;
            grid-template-columns: 110px minmax(0, 1fr);
            gap: 10px 14px;
            margin: 0;
        }

        dt {
            color: var(--muted);
            font-weight: 700;
        }

        dd {
            margin: 0;
        }

        .empty,
        .empty-detail {
            color: var(--muted);
        }

        @media (max-width: 900px) {
            .page-shell,
            .workspace {
                grid-template-columns: 1fr;
            }

            .sidebar {
                border-right: 0;
                border-bottom: 1px solid var(--line);
            }

            .metric-grid {
                grid-template-columns: repeat(2, minmax(0, 1fr));
            }
        }
    </style>
</head>
<body>
    <div class="page-shell">
        <aside class="sidebar">
            <h1>Customer Dashboard</h1>
            <p>Classic ASP sample with embedded VBScript, HTML, CSS, and JavaScript regions.</p>
            <%= RenderSampleNavigation("dashboard") %>

            <form method="get" action="default.asp" class="filter-stack" id="customerFilter">
                <div class="field">
                    <label for="q">Search</label>
                    <input type="search" id="q" name="q" value="<%= Server.HTMLEncode(filter.Query) %>" placeholder="name, owner, or note">
                </div>

                <div class="field">
                    <label for="tier">Tier</label>
                    <select id="tier" name="tier">
                        <%= RenderTierOptions(filter.Tier) %>
                    </select>
                </div>

                <label class="check-field">
                    <input type="checkbox" name="inactive" value="1" <%= CheckedAttribute(filter.IncludeInactive) %>>
                    Include paused accounts
                </label>

                <button type="submit">Apply filters</button>
            </form>
        </aside>

        <main class="content">
            <section class="notice" aria-live="polite">
                <span><strong><%= Server.HTMLEncode(notification) %></strong> are visible.</span>
                <span id="clientClock">Client clock pending</span>
            </section>

            <section class="metric-grid" aria-label="Customer metrics">
                <% RenderMetricCards metrics %>
            </section>

            <section class="workspace">
                <div class="table-panel">
                    <table>
                        <thead>
                            <tr>
                                <th>Customer</th>
                                <th>Tier</th>
                                <th>Owner</th>
                                <th>Balance</th>
                                <th>Next review</th>
                                <th>Status</th>
                            </tr>
                        </thead>
                        <tbody>
                            <%= RenderCustomerRows(filteredCustomers, selectedCustomerId) %>
                        </tbody>
                    </table>
                </div>

                <%= RenderCustomerDetail(selectedCustomer) %>
            </section>
        </main>
    </div>

    <script>
        const formatter = new Intl.DateTimeFormat("en", {
            dateStyle: "medium",
            timeStyle: "short"
        });

        const clock = document.querySelector("#clientClock");
        const selectedRow = document.querySelector(".customer-row.is-selected");

        if (clock) {
            clock.textContent = formatter.format(new Date());
        }

        if (selectedRow) {
            selectedRow.scrollIntoView({ block: "nearest" });
        }

        document.querySelectorAll(".customer-row").forEach((row) => {
            row.addEventListener("mouseenter", () => {
                row.classList.add("is-hovered");
            });

            row.addEventListener("mouseleave", () => {
                row.classList.remove("is-hovered");
            });
        });
    </script>
</body>
</html>
