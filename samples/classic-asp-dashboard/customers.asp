<%@ Language="VBScript" CodePage=65001 %>
<% Option Explicit %>
<!-- #include file="includes/data.inc" -->
<!-- #include file="includes/layout.inc" -->
<%
Dim filter
Set filter = ReadDashboardFilter()

Dim customers
customers = FilterCustomers(BuildCustomerFixtures(), filter)

Function RenderCustomerCards(ByVal customerList)
    Dim html
    html = ""

    If HasItems(customerList) Then
        Dim index
        For index = 0 To UBound(customerList)
            html = html & RenderCustomerCard(customerList(index), index)
        Next
    End If

    If html = "" Then
        html = "<p class=""empty"">No customers found. Try including paused accounts or clearing the search box.</p>"
    End If

    RenderCustomerCards = html
End Function

Function RenderCustomerCard(ByVal customer, ByVal index)
    Dim cardClass
    cardClass = "customer-card"

    If customer.IsOverdue Then
        cardClass = cardClass & " overdue"
    End If

    RenderCustomerCard = _
        "<article class=""" & cardClass & """ data-index=""" & index & """>" & _
            "<header>" & _
                "<h2>" & Server.HTMLEncode(customer.DisplayName) & "</h2>" & _
                "<span>" & Server.HTMLEncode(customer.TierLabel) & "</span>" & _
            "</header>" & _
            "<p>" & Server.HTMLEncode(customer.Notes) & "</p>" & _
            "<dl>" & _
                "<dt>Owner</dt><dd>" & Server.HTMLEncode(customer.Owner) & "</dd>" & _
                "<dt>Email</dt><dd>" & Server.HTMLEncode(customer.Email) & "</dd>" & _
                "<dt>Balance</dt><dd>" & Server.HTMLEncode(FormatCurrencyValue(customer.Balance)) & "</dd>" & _
                "<dt>Status</dt><dd>" & Server.HTMLEncode(customer.StatusText) & "</dd>" & _
            "</dl>" & _
            "<a class=""details-link"" href=""default.asp?customer=" & Server.URLEncode(customer.Id) & """>Open in dashboard</a>" & _
        "</article>"
End Function
%>
<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Customers - Classic ASP Sample</title>
    <style>
        body {
            margin: 0;
            font-family: "Inter", "Segoe UI", sans-serif;
            background: #f7f5ef;
            color: #1d2733;
        }

        .shell {
            display: grid;
            grid-template-columns: 260px minmax(0, 1fr);
            min-height: 100vh;
        }

        aside {
            padding: 28px;
            border-right: 1px solid #d8d0c2;
            background: #fff;
        }

        main {
            padding: 30px;
        }

        .sample-nav {
            display: grid;
            gap: 8px;
            margin: 24px 0;
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

        .filters {
            display: grid;
            gap: 12px;
        }

        input,
        select,
        button {
            min-height: 38px;
            border: 1px solid #d8d0c2;
            border-radius: 6px;
            font: inherit;
        }

        button {
            background: #236b5f;
            color: #fff;
            cursor: pointer;
        }

        .cards {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
            gap: 16px;
        }

        .customer-card {
            display: grid;
            gap: 14px;
            padding: 18px;
            border: 1px solid #d8d0c2;
            border-radius: 8px;
            background: #fff;
        }

        .customer-card.overdue {
            border-color: #d28b8b;
        }

        .customer-card header {
            display: flex;
            justify-content: space-between;
            gap: 12px;
        }

        .customer-card h2 {
            margin: 0;
            font-size: 1.05rem;
        }

        .customer-card p {
            margin: 0;
            color: #68727f;
        }

        dl {
            display: grid;
            grid-template-columns: 84px minmax(0, 1fr);
            gap: 8px;
            margin: 0;
        }

        dt {
            color: #68727f;
            font-weight: 700;
        }

        dd {
            margin: 0;
        }

        .details-link {
            color: #236b5f;
            font-weight: 700;
        }

        @media (max-width: 760px) {
            .shell {
                grid-template-columns: 1fr;
            }

            aside {
                border-right: 0;
                border-bottom: 1px solid #d8d0c2;
            }
        }
    </style>
</head>
<body>
    <div class="shell">
        <aside>
            <h1>Customers</h1>
            <%= RenderSampleNavigation("customers") %>
            <form action="customers.asp" method="get" class="filters">
                <label for="q">Search</label>
                <input id="q" name="q" value="<%= Server.HTMLEncode(filter.Query) %>">

                <label for="tier">Tier</label>
                <select id="tier" name="tier">
                    <%= RenderTierOptions(filter.Tier) %>
                </select>

                <label>
                    <input type="checkbox" name="inactive" value="1" <%= CheckedAttribute(filter.IncludeInactive) %>>
                    Include paused accounts
                </label>

                <button type="submit">Refresh list</button>
            </form>
        </aside>

        <main>
            <section class="cards" aria-label="Customer cards">
                <%= RenderCustomerCards(customers) %>
            </section>
        </main>
    </div>

    <script>
        for (const card of document.querySelectorAll(".customer-card")) {
            card.addEventListener("click", () => {
                card.toggleAttribute("data-open");
            });
        }
    </script>
</body>
</html>
