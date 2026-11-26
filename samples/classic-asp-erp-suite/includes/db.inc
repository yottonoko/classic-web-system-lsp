<%
' =============================================================================
' Database Access Layer (ADODB Wrapper & Safe Query Builders)
' =============================================================================

Function DbOpenConnection(connStr)
    Dim conn
    Set conn = Server.CreateObject("ADODB.Connection")
    conn.ConnectionTimeout = DB_CONNECT_TIMEOUT
    conn.CommandTimeout = DB_COMMAND_TIMEOUT
    conn.Open connStr
    Set DbOpenConnection = conn
End Function

Sub DbCloseConnection(ByRef conn)
    If IsObject(conn) Then
        If Not (conn Is Nothing) Then
            If conn.State <> 0 Then
                conn.Close
            End If
            Set conn = Nothing
        End If
    End If
End Sub

Sub DbCloseRecordset(ByRef rs)
    If IsObject(rs) Then
        If Not (rs Is Nothing) Then
            If rs.State <> 0 Then
                rs.Close
            End If
            Set rs = Nothing
        End If
    End If
End Sub

Function DbQuery(conn, sqlText)
    Dim rs
    Set rs = Server.CreateObject("ADODB.Recordset")
    rs.CursorLocation = 3 ' adUseClient
    rs.Open sqlText, conn, 3, 1 ' adOpenStatic, adLockReadOnly
    Set DbQuery = rs
End Function

Function DbExecScalar(conn, sqlText, defaultValue)
    Dim rs, result
    Set rs = DbQuery(conn, sqlText)
    If Not rs.EOF Then
        If IsNull(rs.Fields(0).Value) Then
            result = defaultValue
        Else
            result = rs.Fields(0).Value
        End If
    Else
        result = defaultValue
    End If
    DbCloseRecordset rs
    DbExecScalar = result
End Function

Function SafeSqlString(rawVal)
    If IsNull(rawVal) Or IsEmpty(rawVal) Then
        SafeSqlString = "NULL"
    Else
        SafeSqlString = "'" & Replace(CStr(rawVal), "'", "''") & "'"
    End If
End Function

Function SafeSqlInt(rawVal, fallbackVal)
    If IsNumeric(rawVal) Then
        SafeSqlInt = CLng(rawVal)
    Else
        SafeSqlInt = CLng(fallbackVal)
    End If
End Function

Function BuildSafeOrderBy(requestedColumn, requestedDir, allowedColumns, defaultOrder)
    Dim col, dir, i, matched
    col = LCase(Trim(CStr(requestedColumn & "")))
    dir = UCase(Trim(CStr(requestedDir & "")))
    If dir <> "ASC" And dir <> "DESC" Then
        dir = "DESC"
    End If

    matched = ""
    For i = LBound(allowedColumns) To UBound(allowedColumns)
        If LCase(allowedColumns(i)) = col Then
            matched = allowedColumns(i)
            Exit For
        End If
    Next

    If matched = "" Then
        BuildSafeOrderBy = defaultOrder
    Else
        BuildSafeOrderBy = matched & " " & dir
    End If
End Function

Sub DbBeginTransaction(conn)
    If IsObject(conn) Then
        conn.BeginTrans
    End If
End Sub

Sub DbCommitTransaction(conn)
    If IsObject(conn) Then
        conn.CommitTrans
    End If
End Sub

Sub DbRollbackTransaction(conn)
    On Error Resume Next
    If IsObject(conn) Then
        conn.RollbackTrans
    End If
    On Error GoTo 0
End Sub
%>
