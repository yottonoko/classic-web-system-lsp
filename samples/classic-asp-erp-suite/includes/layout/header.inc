<%
Sub RenderPageHeader(pageTitle, activeNav)
%>
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title><%= HtmlEncode(pageTitle) %> - <%= HtmlEncode(APP_TITLE) %></title>
    <style>
        :root {
            --erp-primary: #2563eb;
            --erp-primary-dark: #1d4ed8;
            --erp-bg: #f8fafc;
            --erp-surface: #ffffff;
            --erp-border: #e2e8f0;
            --erp-text: #0f172a;
            --erp-muted: #64748b;
            --erp-success: #16a34a;
            --erp-warning: #d97706;
            --erp-danger: #dc2626;
        }
        body {
            margin: 0;
            font-family: system-ui, -apple-system, sans-serif;
            background-color: var(--erp-bg);
            color: var(--erp-text);
        }
        .topbar {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 12px 24px;
            background: #0f172a;
            color: #f8fafc;
        }
        .nav-links a {
            color: #cbd5e1;
            text-decoration: none;
            margin-right: 16px;
            font-weight: 500;
        }
        .nav-links a.active {
            color: #60a5fa;
            border-bottom: 2px solid #60a5fa;
            padding-bottom: 4px;
        }
        .container {
            max-width: 1200px;
            margin: 24px auto;
            padding: 0 20px;
        }
        .card {
            background: var(--erp-surface);
            border: 1px solid var(--erp-border);
            border-radius: 8px;
            padding: 18px 20px;
            margin-bottom: 18px;
        }
        .badge {
            display: inline-block;
            padding: 2px 8px;
            border-radius: 999px;
            font-size: 12px;
            font-weight: 600;
        }
        .badge-success { background: #dcfce7; color: #166534; }
        .badge-info { background: #dbeafe; color: #1e40af; }
        .badge-warning { background: #fef3c7; color: #92400e; }
        .badge-danger { background: #fee2e2; color: #991b1b; }
        .badge-neutral { background: #e2e8f0; color: #334155; }
    </style>
</head>
<body>
    <header class="topbar">
        <div><strong><%= HtmlEncode(APP_TITLE) %></strong></div>
        <nav class="nav-links">
            <a href="<%= APP_BASE_URL %>/default.asp" class="<% If activeNav = "dashboard" Then Response.Write "active" %>">Dashboard</a>
            <a href="<%= APP_BASE_URL %>/orders/list.asp" class="<% If activeNav = "orders" Then Response.Write "active" %>">Orders</a>
            <a href="<%= APP_BASE_URL %>/inventory/warehouse.asp" class="<% If activeNav = "inventory" Then Response.Write "active" %>">Inventory</a>
            <a href="<%= APP_BASE_URL %>/billing/invoices.asp" class="<% If activeNav = "billing" Then Response.Write "active" %>">Billing</a>
        </nav>
        <div>User: <%= HtmlEncode(GetCurrentUserName()) %> (<%= HtmlEncode(GetCurrentUserRole()) %>)</div>
    </header>
    <main class="container">
<%
End Sub
%>
