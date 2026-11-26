<%
' =============================================================================
' Application Configuration Constants
' =============================================================================
Const APP_TITLE = "Northwind Classic ERP"
Const APP_ENV = "staging"
Const APP_BASE_URL = "/erp"

Const DB_CONNECTION_STRING = "Provider=SQLOLEDB;Data Source=ERP-DB01;Initial Catalog=NorthwindERP;Integrated Security=SSPI;"
Const DB_COMMAND_TIMEOUT = 30
Const DB_CONNECT_TIMEOUT = 10

Const DEFAULT_PAGE_SIZE = 25
Const MAX_PAGE_SIZE = 200
Const DEFAULT_TAX_RATE = 0.10
Const REDUCED_TAX_RATE = 0.08
Const FREE_SHIPPING_THRESHOLD = 50000

' Order lifecycle status constants
Const ORDER_STATUS_DRAFT = "DRAFT"
Const ORDER_STATUS_PENDING = "PENDING"
Const ORDER_STATUS_APPROVED = "APPROVED"
Const ORDER_STATUS_SHIPPED = "SHIPPED"
Const ORDER_STATUS_COMPLETED = "COMPLETED"
Const ORDER_STATUS_CANCELLED = "CANCELLED"

' Invoice status constants
Const INVOICE_STATUS_UNPAID = "UNPAID"
Const INVOICE_STATUS_PARTIAL = "PARTIAL"
Const INVOICE_STATUS_PAID = "PAID"
Const INVOICE_STATUS_OVERDUE = "OVERDUE"

' Role constants
Const ROLE_ADMIN = "admin"
Const ROLE_MANAGER = "manager"
Const ROLE_OPERATOR = "operator"
Const ROLE_VIEWER = "viewer"

' Warehouse codes
Const WH_TOKYO = "WH-TYO-01"
Const WH_OSAKA = "WH-OSA-02"
Const WH_NAGOYA = "WH-NGO-03"
Const WH_FUKUOKA = "WH-FUK-04"

Function GetAppSetting(keyName, fallbackValue)
    Dim v
    v = Application(keyName)
    If IsEmpty(v) Or IsNull(v) Or CStr(v) = "" Then
        GetAppSetting = fallbackValue
    Else
        GetAppSetting = CStr(v)
    End If
End Function

Function GetStatusLabel(statusCode)
    Select Case UCase(Trim(CStr(statusCode)))
        Case ORDER_STATUS_DRAFT
            GetStatusLabel = "Draft"
        Case ORDER_STATUS_PENDING
            GetStatusLabel = "Pending Approval"
        Case ORDER_STATUS_APPROVED
            GetStatusLabel = "Approved"
        Case ORDER_STATUS_SHIPPED
            GetStatusLabel = "Shipped"
        Case ORDER_STATUS_COMPLETED
            GetStatusLabel = "Completed"
        Case ORDER_STATUS_CANCELLED
            GetStatusLabel = "Cancelled"
        Case Else
            GetStatusLabel = "Unknown"
    End Select
End Function

Function IsTerminalOrderStatus(statusCode)
    Dim normalized
    normalized = UCase(Trim(CStr(statusCode)))
    IsTerminalOrderStatus = (normalized = ORDER_STATUS_COMPLETED Or normalized = ORDER_STATUS_CANCELLED)
End Function
%>
