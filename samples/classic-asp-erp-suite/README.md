# Classic ASP ERP Suite (Verification Sample)

A realistic, medium-large Classic ASP / VBScript multi-file sample project designed to verify every feature of the **Classic ASP LSP** VS Code extension.

## Structure

- `global.asa` — Application and Session lifecycle hooks (`Application_OnStart`, `Session_OnStart`)
- `includes/`
  - `config.inc` — Application constants, status codes, tax & paging defaults
  - `security.inc` — Session authentication, RBAC permissions, CSRF tokens, HTML/JS escaping, audit logging
  - `db.inc` — ADODB connection, transaction, and parameterized query helpers
  - `formatters.inc` — Currency, date, CSV, badge, and pagination helpers
  - `validators.inc` — Input validation helpers for orders, customers, SKUs, and dates
  - `models/customer_model.inc` — `CustomerAccount` class (`Property Get/Let`, credit limit & tier discounts)
  - `models/inventory_model.inc` — `InventoryItem` & `WarehouseStock` classes
  - `models/order_model.inc` — `OrderModel` & `OrderLineItem` classes
  - `services/order_service.inc` — Order lifecycle, stock reservation, and search queries
  - `services/billing_service.inc` — Invoice generation, payment recording, aging breakdown
  - `layout/header.inc`, `layout/footer.inc` — Shared HTML shell, CSS theme colors, and client-side JS
- `default.asp` — Executive ERP dashboard
- `orders/list.asp`, `orders/detail.asp`, `orders/create.asp` — Order management pages
- `inventory/warehouse.asp` — Warehouse inventory & reorder management
- `billing/invoices.asp` — Billing & overdue invoice tracking
- `admin/diagnostics_playground.asp` — Dedicated playground for diagnostics, quick-fixes, CSS colors, folding, and inlay hints
- `scripts/nightly_batch.vbs` — Standalone VBScript nightly batch script

## LSP Feature Verification Checklist

1. **Cross-file Go-to-Definition & Document Links**: Open `orders/detail.asp` or `default.asp`, Ctrl+Click `#include` paths and function/class calls (e.g., `LoadOrderById`, `FormatCurrencyJpy`, `OrderModel`).
2. **CodeLens & Find All References**: Check reference counts above `Sub`/`Function`/`Class` declarations in `includes/formatters.inc` and `includes/services/order_service.inc`.
3. **Inlay Hints & Signature Help**: Inspect parameter name hints on multi-argument calls in `orders/create.asp` and `billing/invoices.asp`.
4. **Hover & Completion**: Hover over `Response.Write`, `Server.CreateObject`, `Session`, `DbQuery`, or `OrderModel`; trigger completion after `Response.`, `order.`, and `Format`.
5. **Diagnostics & Code Actions (Quick Fix)**: Open `admin/diagnostics_playground.asp` to verify diagnostics and quick-fix actions (e.g. adding `Option Explicit` or declaring missing `Dim`).
6. **Folding, Document Highlights, Selection Ranges & Formatting**: Test code folding on `Class`/`Function`/`Select Case`/`If` blocks, symbol highlighting, smart selection expansion, and `Format Document`.
7. **CSS Document Colors & Embedded HTML/JS**: Open `includes/layout/header.inc` and `admin/diagnostics_playground.asp` to see CSS color swatches and HTML/JS support.
