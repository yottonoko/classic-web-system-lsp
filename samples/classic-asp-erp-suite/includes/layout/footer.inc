<%
Sub RenderPageFooter()
%>
    </main>
    <footer style="text-align:center; padding: 24px; color: #64748b; font-size: 13px;">
        <%= HtmlEncode(APP_TITLE) %> (<%= HtmlEncode(APP_ENV) %>) &mdash; Rendered at <%= HtmlEncode(FormatIsoDateTime(Now())) %>
    </footer>
    <script>
        (function () {
            const buttons = document.querySelectorAll('[data-confirm]');
            buttons.forEach(function (btn) {
                btn.addEventListener('click', function (ev) {
                    const msg = btn.getAttribute('data-confirm') || 'Continue?';
                    if (!window.confirm(msg)) {
                        ev.preventDefault();
                    }
                });
            });
        })();
    </script>
</body>
</html>
<%
End Sub
%>
