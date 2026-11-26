Option Explicit

' =============================================================================
' Nightly ERP Batch Utility (Standalone VBScript .vbs)
' Demonstrates Class, Property, Sub, Function, FileSystemObject, and WScript.
' =============================================================================

Const BATCH_VERSION = "2.4.0"
Const MAX_LOG_FILES = 14

Class BatchSummary
    Private m_ProcessedCount
    Private m_ErrorCount
    Private m_TotalRevenue

    Private Sub Class_Initialize()
        m_ProcessedCount = 0
        m_ErrorCount = 0
        m_TotalRevenue = 0
    End Sub

    Public Property Get ProcessedCount()
        ProcessedCount = m_ProcessedCount
    End Property

    Public Property Get ErrorCount()
        ErrorCount = m_ErrorCount
    End Property

    Public Property Get TotalRevenue()
        TotalRevenue = m_TotalRevenue
    End Property

    Public Sub RecordSuccess(amount)
        m_ProcessedCount = m_ProcessedCount + 1
        m_TotalRevenue = m_TotalRevenue + CDbl(amount)
    End Sub

    Public Sub RecordFailure()
        m_ErrorCount = m_ErrorCount + 1
    End Sub

    Public Function FormatReport()
        FormatReport = "Batch v" & BATCH_VERSION & _
                       " | Processed=" & CStr(m_ProcessedCount) & _
                       " | Errors=" & CStr(m_ErrorCount) & _
                       " | Revenue=" & FormatNumber(m_TotalRevenue, 0, -1, 0, -1)
    End Function
End Class

Function ComputeChecksum(textValue)
    Dim i, acc
    acc = 0
    For i = 1 To Len(textValue)
        acc = (acc * 31 + Asc(Mid(textValue, i, 1))) Mod 65521
    Next
    ComputeChecksum = acc
End Function

Sub RunNightlyBatch(targetDir)
    Dim summary, sampleAmounts, i, amount, reportText
    Set summary = New BatchSummary
    sampleAmounts = Array(125000, 84200, 312000, 0, 56000)

    For i = LBound(sampleAmounts) To UBound(sampleAmounts)
        amount = sampleAmounts(i)
        If amount > 0 Then
            summary.RecordSuccess amount
        Else
            summary.RecordFailure
        End If
    Next

    reportText = summary.FormatReport() & " | Dir=" & targetDir & " | Checksum=" & CStr(ComputeChecksum(targetDir))
    WScript.Echo reportText
End Sub

RunNightlyBatch "C:\ERP\Exports\Nightly"
