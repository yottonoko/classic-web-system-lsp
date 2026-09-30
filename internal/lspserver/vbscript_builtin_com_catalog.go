package lspserver

import (
	"context"
	"strings"
	"sync"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

// vbComStubType describes one built-in COM type. Member signatures are written
// as a bare parameter list; parameters wrapped in brackets are optional.
type vbComStubType struct {
	name    string
	detail  string
	aliases []string
	members []vbBuiltinMember
}

func comProp(name, typeName, en, ja string) vbBuiltinMember {
	return vbBuiltinMember{Name: name, Kind: lsp.CompletionItemKindProperty, TypeName: typeName, Docs: localizedBuiltinDocs{EN: en, JA: ja}}
}

func comIndexed(name, typeName, params, en, ja string) vbBuiltinMember {
	return vbBuiltinMember{Name: name, Kind: lsp.CompletionItemKindProperty, TypeName: typeName, Signature: "(" + params + ")", Docs: localizedBuiltinDocs{EN: en, JA: ja}}
}

func comMethod(name, returnType, params, en, ja string) vbBuiltinMember {
	return vbBuiltinMember{Name: name, Kind: lsp.CompletionItemKindMethod, TypeName: returnType, Signature: "(" + params + ")", Docs: localizedBuiltinDocs{EN: en, JA: ja}}
}

var vbscriptBuiltinComCatalog = sync.OnceValue(func() map[string]map[string]vbBuiltinMember {
	catalog := map[string]map[string]vbBuiltinMember{}
	for _, stub := range vbscriptBuiltinComStubTypes() {
		members := make(map[string]vbBuiltinMember, len(stub.members))
		for _, member := range stub.members {
			if member.Signature != "" {
				member.Signature = stub.detail + "." + member.Name + member.Signature
			}
			members[strings.ToLower(member.Name)] = member
		}
		catalog[strings.ToLower(stub.name)] = members
		for _, alias := range stub.aliases {
			catalog[strings.ToLower(alias)] = members
		}
	}
	return catalog
})

// vbscriptBuiltinComTypeMembers returns the shared member table for a built-in
// COM type. Callers must not mutate the result.
func vbscriptBuiltinComTypeMembers(lowerTypeName string) map[string]vbBuiltinMember {
	catalog := vbscriptBuiltinComCatalog()
	if members, ok := catalog[lowerTypeName]; ok {
		return members
	}
	if base := trimVBProgIDVersion(lowerTypeName); base != lowerTypeName {
		return catalog[base]
	}
	return nil
}

// trimVBProgIDVersion drops a version-dependent ProgID suffix such as ".6.0"
// so "MSXML2.ServerXMLHTTP.6.0" resolves to the version-independent stub.
func trimVBProgIDVersion(progID string) string {
	end := len(progID)
	for end > 0 {
		cursor := end
		for cursor > 0 && progID[cursor-1] >= '0' && progID[cursor-1] <= '9' {
			cursor--
		}
		if cursor == end || cursor == 0 || progID[cursor-1] != '.' {
			break
		}
		end = cursor - 1
	}
	return progID[:end]
}

func vbscriptBuiltinComStubTypes() []vbComStubType {
	types := []vbComStubType{}
	types = append(types, vbscriptADOStubTypes()...)
	types = append(types, vbscriptScriptingStubTypes()...)
	types = append(types, vbscriptXMLStubTypes()...)
	types = append(types, vbscriptMailStubTypes()...)
	types = append(types, vbscriptWSHStubTypes()...)
	return types
}

func vbscriptADOStubTypes() []vbComStubType {
	getRows := comMethod("GetRows", "Array", "[rows], [start], [fields]",
		"Copies records from a Recordset into a two-dimensional array.",
		"Recordset から records を 2 次元 array へ copy します。")
	getRows.Parameters = []localizedBuiltinParameter{{
		Name:     "rows",
		Optional: true,
		EN:       "Number of records to retrieve. When omitted, retrieves the remaining records in the Recordset.",
		JA:       "取得する records 数です。省略すると Recordset の残りを取得します。",
	}, {
		Name:     "start",
		Optional: true,
		EN:       "Record number or bookmark where copying starts.",
		JA:       "copy を開始する record number または bookmark です。",
	}, {
		Name:     "fields",
		Optional: true,
		EN:       "Field name/number, or an array of field names/numbers to include.",
		JA:       "含める field name/number、または field names/numbers の array です。",
	}}
	return []vbComStubType{
		{name: "ADODB.Connection", detail: "Connection", members: []vbBuiltinMember{
			comProp("Attributes", "Number", "Transaction attributes (adXactCommitRetaining, adXactAbortRetaining).", "トランザクションの属性 (adXactCommitRetaining, adXactAbortRetaining) です。"),
			comProp("CommandTimeout", "Number", "Seconds to wait for a command to execute before raising an error. Default is 30.", "コマンド実行の待ち時間 (秒) です。既定値は 30 です。"),
			comProp("ConnectionString", "String", "Information used to establish the connection to the data source.", "データソースへの接続に使う接続文字列です。"),
			comProp("ConnectionTimeout", "Number", "Seconds to wait while establishing a connection. Default is 15.", "接続確立の待ち時間 (秒) です。既定値は 15 です。"),
			comProp("CursorLocation", "Number", "Location of the cursor service (adUseServer or adUseClient).", "カーソルサービスの場所 (adUseServer または adUseClient) です。"),
			comProp("DefaultDatabase", "String", "Default database for the connection.", "接続の既定のデータベースです。"),
			comProp("Errors", "ADODB.Errors", "Collection of provider errors raised by the last operation.", "直前の操作でプロバイダーが返したエラーのコレクションです。"),
			comProp("IsolationLevel", "Number", "Transaction isolation level.", "トランザクションの分離レベルです。"),
			comProp("Mode", "Number", "Permissions for modifying data (adModeRead, adModeReadWrite, ...).", "データ変更の権限 (adModeRead, adModeReadWrite など) です。"),
			comProp("Properties", "ADODB.Properties", "Provider-specific dynamic properties.", "プロバイダー固有の動的プロパティです。"),
			comProp("Provider", "String", "Name of the OLE DB provider.", "OLE DB プロバイダーの名前です。"),
			comProp("State", "Number", "Whether the connection is open (adStateOpen) or closed (adStateClosed).", "接続が開いている (adStateOpen) か閉じている (adStateClosed) かを示します。"),
			comProp("Version", "String", "ADO version number.", "ADO のバージョン番号です。"),
			comMethod("BeginTrans", "Number", "", "Begins a new transaction and returns the nesting level.", "新しいトランザクションを開始し、ネストレベルを返します。"),
			comMethod("Cancel", "Variant", "", "Cancels a pending asynchronous Execute or Open call.", "保留中の非同期 Execute または Open をキャンセルします。"),
			comMethod("Close", "Variant", "", "Closes the connection and any dependent objects.", "接続と依存するオブジェクトを閉じます。"),
			comMethod("CommitTrans", "Variant", "", "Saves changes and ends the current transaction.", "変更を保存して現在のトランザクションを終了します。"),
			comMethod("Execute", "ADODB.Recordset", "commandText, [recordsAffected], [options]", "Executes a query, SQL statement, or stored procedure and returns a forward-only, read-only Recordset.", "クエリ、SQL 文、またはストアドプロシージャを実行し、前方専用・読み取り専用の Recordset を返します。"),
			comMethod("Open", "Variant", "[connectionString], [userID], [password], [options]", "Opens a connection to a data source.", "データソースへの接続を開きます。"),
			comMethod("OpenSchema", "ADODB.Recordset", "schema, [restrictions], [schemaID]", "Returns schema information from the provider as a Recordset.", "プロバイダーのスキーマ情報を Recordset として返します。"),
			comMethod("RollbackTrans", "Variant", "", "Cancels changes made during the current transaction and ends it.", "現在のトランザクション中の変更を取り消して終了します。"),
		}},
		{name: "ADODB.Recordset", detail: "Recordset", members: []vbBuiltinMember{
			comProp("AbsolutePage", "Number", "Page on which the current record resides.", "現在のレコードがあるページ番号です。"),
			comProp("AbsolutePosition", "Number", "Ordinal position of the current record.", "現在のレコードの位置 (序数) です。"),
			comProp("ActiveCommand", "ADODB.Command", "Command object that created the Recordset.", "この Recordset を作成した Command オブジェクトです。"),
			comProp("ActiveConnection", "Variant", "Connection object or connection string the Recordset belongs to.", "Recordset が属する Connection オブジェクトまたは接続文字列です。"),
			comProp("BOF", "Boolean", "True when the current position is before the first record.", "現在位置が最初のレコードより前のとき True です。"),
			comProp("Bookmark", "Variant", "Bookmark that identifies the current record.", "現在のレコードを識別するブックマークです。"),
			comProp("CacheSize", "Number", "Number of records cached locally in memory.", "メモリにキャッシュするレコード数です。"),
			comProp("CursorLocation", "Number", "Location of the cursor service (adUseServer or adUseClient).", "カーソルサービスの場所 (adUseServer または adUseClient) です。"),
			comProp("CursorType", "Number", "Type of cursor (adOpenForwardOnly, adOpenKeyset, adOpenDynamic, adOpenStatic).", "カーソルの種類 (adOpenForwardOnly, adOpenKeyset, adOpenDynamic, adOpenStatic) です。"),
			comProp("DataMember", "String", "Name of the data member retrieved from the DataSource.", "DataSource から取得するデータメンバーの名前です。"),
			comProp("DataSource", "Object", "Object that contains the data represented by the Recordset.", "Recordset が表すデータを持つオブジェクトです。"),
			comProp("EditMode", "Number", "Editing status of the current record.", "現在のレコードの編集状態です。"),
			comProp("EOF", "Boolean", "True when the current position is after the last record.", "現在位置が最後のレコードより後のとき True です。"),
			comProp("Fields", "ADODB.Fields", "Collection of Field objects for the current record.", "現在のレコードの Field オブジェクトのコレクションです。"),
			comProp("Filter", "Variant", "Filter applied to the records (criteria string, bookmarks, or a FilterGroupEnum value).", "レコードに適用するフィルター (条件文字列、ブックマーク、または FilterGroupEnum 値) です。"),
			comProp("Index", "String", "Name of the index currently in effect.", "現在有効なインデックスの名前です。"),
			comProp("LockType", "Number", "Type of lock placed on records during editing (adLockReadOnly, adLockOptimistic, ...).", "編集時のロックの種類 (adLockReadOnly, adLockOptimistic など) です。"),
			comProp("MarshalOptions", "Number", "Which records are marshaled back to the server.", "サーバーへ戻すレコードの範囲です。"),
			comProp("MaxRecords", "Number", "Maximum number of records returned by a query. 0 means no limit.", "クエリが返すレコード数の上限です。0 は無制限です。"),
			comProp("PageCount", "Number", "Number of pages of data the Recordset contains.", "Recordset に含まれるページ数です。"),
			comProp("PageSize", "Number", "Number of records that make up one page.", "1 ページあたりのレコード数です。"),
			comProp("Properties", "ADODB.Properties", "Provider-specific dynamic properties.", "プロバイダー固有の動的プロパティです。"),
			comProp("RecordCount", "Number", "Number of records. Returns -1 when the cursor type cannot count records.", "レコード数です。カーソルの種類によっては -1 を返します。"),
			comProp("Sort", "String", "Comma-separated field names the Recordset is sorted on.", "並べ替えに使うフィールド名 (カンマ区切り) です。"),
			comProp("Source", "Variant", "Source of the data (Command object, SQL statement, table name, or stored procedure).", "データの取得元 (Command オブジェクト、SQL 文、テーブル名、ストアドプロシージャ) です。"),
			comProp("State", "Number", "Whether the Recordset is open, closed, or executing.", "Recordset が開いているか、閉じているか、実行中かを示します。"),
			comProp("Status", "Number", "Status of the current record with respect to batch updates.", "バッチ更新に関する現在のレコードの状態です。"),
			comProp("StayInSync", "Boolean", "Whether child record references change when the parent row position changes.", "親の行位置が変わったとき子レコードの参照も変えるかどうかです。"),
			comMethod("AddNew", "Variant", "[fieldList], [values]", "Creates a new record in an updatable Recordset.", "更新可能な Recordset に新しいレコードを作成します。"),
			comMethod("Cancel", "Variant", "", "Cancels a pending asynchronous Open call.", "保留中の非同期 Open をキャンセルします。"),
			comMethod("CancelBatch", "Variant", "[affectRecords]", "Cancels a pending batch update.", "保留中のバッチ更新をキャンセルします。"),
			comMethod("CancelUpdate", "Variant", "", "Cancels changes made to the current or new record before Update is called.", "Update を呼ぶ前の現在または新規レコードへの変更を取り消します。"),
			comMethod("Clone", "ADODB.Recordset", "[lockType]", "Creates a duplicate Recordset.", "Recordset の複製を作成します。"),
			comMethod("Close", "Variant", "", "Closes the Recordset.", "Recordset を閉じます。"),
			comMethod("CompareBookmarks", "Number", "bookmark1, bookmark2", "Compares two bookmarks and returns their relative position.", "2 つのブックマークを比較して相対位置を返します。"),
			comMethod("Delete", "Variant", "[affectRecords]", "Deletes the current record or a group of records.", "現在のレコードまたはレコードのグループを削除します。"),
			comMethod("Find", "Variant", "criteria, [skipRows], [searchDirection], [start]", "Searches for the record that satisfies the criteria.", "条件に合うレコードを検索します。"),
			getRows,
			comMethod("GetString", "String", "[stringFormat], [numRows], [columnDelimiter], [rowDelimiter], [nullExpr]", "Returns the Recordset as a string.", "Recordset を文字列として返します。"),
			comMethod("Move", "Variant", "numRecords, [start]", "Moves the position of the current record.", "現在のレコード位置を移動します。"),
			comMethod("MoveFirst", "Variant", "", "Moves to the first record.", "最初のレコードへ移動します。"),
			comMethod("MoveLast", "Variant", "", "Moves to the last record.", "最後のレコードへ移動します。"),
			comMethod("MoveNext", "Variant", "", "Moves to the next record.", "次のレコードへ移動します。"),
			comMethod("MovePrevious", "Variant", "", "Moves to the previous record.", "前のレコードへ移動します。"),
			comMethod("NextRecordset", "ADODB.Recordset", "[recordsAffected]", "Clears the current Recordset and returns the next one from a compound command.", "現在の Recordset をクリアし、複合コマンドの次の Recordset を返します。"),
			comMethod("Open", "Variant", "[source], [activeConnection], [cursorType], [lockType], [options]", "Opens a cursor on a query, table, or stored procedure.", "クエリ、テーブル、またはストアドプロシージャに対してカーソルを開きます。"),
			comMethod("Requery", "Variant", "[options]", "Refreshes the data by re-executing the query.", "クエリを再実行してデータを更新します。"),
			comMethod("Resync", "Variant", "[affectRecords], [resyncValues]", "Refreshes the data from the underlying database.", "基になるデータベースからデータを再取得します。"),
			comMethod("Save", "Variant", "[destination], [persistFormat]", "Saves the Recordset to a file or Stream object.", "Recordset をファイルまたは Stream オブジェクトに保存します。"),
			comMethod("Seek", "Variant", "keyValues, [seekOption]", "Searches the index and moves to the matching row.", "インデックスを検索して一致する行へ移動します。"),
			comMethod("Supports", "Boolean", "cursorOptions", "Whether the Recordset supports the given functionality.", "Recordset が指定した機能をサポートするかどうかを返します。"),
			comMethod("Update", "Variant", "[fields], [values]", "Saves changes made to the current record.", "現在のレコードへの変更を保存します。"),
			comMethod("UpdateBatch", "Variant", "[affectRecords]", "Writes all pending batch updates to the database.", "保留中のバッチ更新をすべてデータベースへ書き込みます。"),
		}},
		{name: "ADODB.Command", detail: "Command", members: []vbBuiltinMember{
			comProp("ActiveConnection", "Variant", "Connection object or connection string the command runs on.", "コマンドを実行する Connection オブジェクトまたは接続文字列です。"),
			comProp("CommandStream", "Variant", "Stream used as the input for the command.", "コマンドの入力に使うストリームです。"),
			comProp("CommandText", "String", "SQL statement, table name, or stored procedure to execute.", "実行する SQL 文、テーブル名、またはストアドプロシージャです。"),
			comProp("CommandTimeout", "Number", "Seconds to wait for the command to execute. Default is 30.", "コマンド実行の待ち時間 (秒) です。既定値は 30 です。"),
			comProp("CommandType", "Number", "Type of command (adCmdText, adCmdTable, adCmdStoredProc, ...).", "コマンドの種類 (adCmdText, adCmdTable, adCmdStoredProc など) です。"),
			comProp("Dialect", "String", "Dialect of the CommandText or CommandStream.", "CommandText または CommandStream の方言です。"),
			comProp("Name", "String", "Name of the command.", "コマンドの名前です。"),
			comProp("NamedParameters", "Boolean", "Whether parameter names are passed to the provider.", "パラメーター名をプロバイダーへ渡すかどうかです。"),
			comProp("Parameters", "ADODB.Parameters", "Collection of Parameter objects.", "Parameter オブジェクトのコレクションです。"),
			comProp("Prepared", "Boolean", "Whether to save a compiled version of the command before execution.", "実行前にコンパイル済みのコマンドを保存するかどうかです。"),
			comProp("Properties", "ADODB.Properties", "Provider-specific dynamic properties.", "プロバイダー固有の動的プロパティです。"),
			comProp("State", "Number", "Whether the command is open, closed, or executing.", "コマンドが開いているか、閉じているか、実行中かを示します。"),
			comMethod("Cancel", "Variant", "", "Cancels a pending asynchronous Execute call.", "保留中の非同期 Execute をキャンセルします。"),
			comMethod("CreateParameter", "ADODB.Parameter", "[name], [type], [direction], [size], [value]", "Creates a new Parameter object. Append it to the Parameters collection to use it.", "新しい Parameter オブジェクトを作成します。使うには Parameters コレクションへ Append します。"),
			comMethod("Execute", "ADODB.Recordset", "[recordsAffected], [parameters], [options]", "Executes the query, SQL statement, or stored procedure in CommandText.", "CommandText のクエリ、SQL 文、またはストアドプロシージャを実行します。"),
		}},
		{name: "ADODB.Parameters", detail: "Parameters", members: []vbBuiltinMember{
			comProp("Count", "Number", "Number of parameters in the collection.", "コレクション内のパラメーター数です。"),
			comIndexed("Item", "ADODB.Parameter", "index", "Returns a parameter by name or ordinal.", "名前または序数でパラメーターを返します。"),
			comMethod("Append", "Variant", "object", "Appends a Parameter object to the collection.", "Parameter オブジェクトをコレクションへ追加します。"),
			comMethod("Delete", "Variant", "index", "Deletes a parameter from the collection.", "コレクションからパラメーターを削除します。"),
			comMethod("Refresh", "Variant", "", "Retrieves parameter information from the provider.", "プロバイダーからパラメーター情報を取得します。"),
		}},
		{name: "ADODB.Parameter", detail: "Parameter", members: []vbBuiltinMember{
			comProp("Attributes", "Number", "Characteristics of the parameter.", "パラメーターの特性です。"),
			comProp("Direction", "Number", "Whether the parameter is input, output, both, or a return value.", "パラメーターが入力、出力、入出力、戻り値のどれかを示します。"),
			comProp("Name", "String", "Name of the parameter.", "パラメーターの名前です。"),
			comProp("NumericScale", "Number", "Number of decimal places for numeric values.", "数値の小数点以下の桁数です。"),
			comProp("Precision", "Number", "Maximum number of digits for numeric values.", "数値の最大桁数です。"),
			comProp("Properties", "ADODB.Properties", "Provider-specific dynamic properties.", "プロバイダー固有の動的プロパティです。"),
			comProp("Size", "Number", "Maximum size of the value in bytes or characters.", "値の最大サイズ (バイトまたは文字数) です。"),
			comProp("Type", "Number", "Data type of the parameter (adInteger, adVarChar, ...).", "パラメーターのデータ型 (adInteger, adVarChar など) です。"),
			comProp("Value", "Variant", "Value assigned to the parameter.", "パラメーターに割り当てる値です。"),
			comMethod("AppendChunk", "Variant", "data", "Appends data to a large text or binary parameter.", "大きなテキストまたはバイナリのパラメーターへデータを追加します。"),
		}},
		{name: "ADODB.Fields", detail: "Fields", members: []vbBuiltinMember{
			comProp("Count", "Number", "Number of fields in the collection.", "コレクション内のフィールド数です。"),
			comIndexed("Item", "ADODB.Field", "index", "Returns a field by name or ordinal.", "名前または序数でフィールドを返します。"),
			comMethod("Append", "Variant", "name, type, [definedSize], [attrib], [fieldValue]", "Appends a field to the collection.", "コレクションへフィールドを追加します。"),
			comMethod("CancelUpdate", "Variant", "", "Cancels pending changes to the Fields collection.", "Fields コレクションへの保留中の変更を取り消します。"),
			comMethod("Delete", "Variant", "index", "Deletes a field from the collection.", "コレクションからフィールドを削除します。"),
			comMethod("Refresh", "Variant", "", "Updates the fields in the collection.", "コレクション内のフィールドを更新します。"),
			comMethod("Resync", "Variant", "[resyncValues]", "Resynchronizes the field values with the data source.", "フィールドの値をデータソースと再同期します。"),
			comMethod("Update", "Variant", "", "Saves pending changes to the Fields collection.", "Fields コレクションへの保留中の変更を保存します。"),
		}},
		{name: "ADODB.Field", detail: "Field", members: []vbBuiltinMember{
			comProp("ActualSize", "Number", "Actual length of the field value.", "フィールド値の実際の長さです。"),
			comProp("Attributes", "Number", "Characteristics of the field.", "フィールドの特性です。"),
			comProp("DataFormat", "Variant", "Format applied to the field value.", "フィールド値に適用する書式です。"),
			comProp("DefinedSize", "Number", "Defined size of the field.", "フィールドの定義サイズです。"),
			comProp("Name", "String", "Name of the field.", "フィールドの名前です。"),
			comProp("NumericScale", "Number", "Number of decimal places for numeric values.", "数値の小数点以下の桁数です。"),
			comProp("OriginalValue", "Variant", "Value of the field before any changes were made.", "変更前のフィールド値です。"),
			comProp("Precision", "Number", "Maximum number of digits for numeric values.", "数値の最大桁数です。"),
			comProp("Properties", "ADODB.Properties", "Provider-specific dynamic properties.", "プロバイダー固有の動的プロパティです。"),
			comProp("Status", "Number", "Status of the field.", "フィールドの状態です。"),
			comProp("Type", "Number", "Data type of the field (adInteger, adVarChar, ...).", "フィールドのデータ型 (adInteger, adVarChar など) です。"),
			comProp("UnderlyingValue", "Variant", "Current value of the field in the database.", "データベース上の現在のフィールド値です。"),
			comProp("Value", "Variant", "Value of the field. This is the default property.", "フィールドの値です。既定のプロパティです。"),
			comMethod("AppendChunk", "Variant", "data", "Appends data to a large text or binary field.", "大きなテキストまたはバイナリのフィールドへデータを追加します。"),
			comMethod("GetChunk", "Variant", "size", "Returns part of a large text or binary field.", "大きなテキストまたはバイナリのフィールドの一部を返します。"),
		}},
		{name: "ADODB.Errors", detail: "Errors", members: []vbBuiltinMember{
			comProp("Count", "Number", "Number of errors in the collection.", "コレクション内のエラー数です。"),
			comIndexed("Item", "ADODB.Error", "index", "Returns an error by ordinal.", "序数でエラーを返します。"),
			comMethod("Clear", "Variant", "", "Removes all errors from the collection.", "コレクションからすべてのエラーを削除します。"),
			comMethod("Refresh", "Variant", "", "Updates the errors in the collection.", "コレクション内のエラーを更新します。"),
		}},
		{name: "ADODB.Error", detail: "Error", members: []vbBuiltinMember{
			comProp("Description", "String", "Descriptive text of the error.", "エラーの説明です。"),
			comProp("HelpContext", "Number", "Help context ID for the error.", "エラーのヘルプコンテキスト ID です。"),
			comProp("HelpFile", "String", "Help file for the error.", "エラーのヘルプファイルです。"),
			comProp("NativeError", "Number", "Provider-specific error code.", "プロバイダー固有のエラーコードです。"),
			comProp("Number", "Number", "Number that uniquely identifies the error.", "エラーを識別する番号です。"),
			comProp("Source", "String", "Name of the object or application that raised the error.", "エラーを発生させたオブジェクトまたはアプリケーションの名前です。"),
			comProp("SQLState", "String", "Five-character ANSI SQL state for the error.", "エラーの ANSI SQL ステート (5 文字) です。"),
		}},
		{name: "ADODB.Properties", detail: "Properties", members: []vbBuiltinMember{
			comProp("Count", "Number", "Number of properties in the collection.", "コレクション内のプロパティ数です。"),
			comIndexed("Item", "ADODB.Property", "index", "Returns a property by name or ordinal.", "名前または序数でプロパティを返します。"),
			comMethod("Refresh", "Variant", "", "Updates the properties in the collection.", "コレクション内のプロパティを更新します。"),
		}},
		{name: "ADODB.Property", detail: "Property", members: []vbBuiltinMember{
			comProp("Attributes", "Number", "Characteristics of the property.", "プロパティの特性です。"),
			comProp("Name", "String", "Name of the property.", "プロパティの名前です。"),
			comProp("Type", "Number", "Data type of the property.", "プロパティのデータ型です。"),
			comProp("Value", "Variant", "Value of the property.", "プロパティの値です。"),
		}},
		{name: "ADODB.Stream", detail: "Stream", members: []vbBuiltinMember{
			comProp("Charset", "String", "Character set used to translate text content, for example \"utf-8\" or \"shift_jis\".", "テキストの変換に使う文字セットです (例: \"utf-8\", \"shift_jis\")。"),
			comProp("EOS", "Boolean", "True when the current position is at the end of the stream.", "現在位置がストリームの終端のとき True です。"),
			comProp("LineSeparator", "Number", "Line separator used by text streams (adCRLF, adLF, adCR).", "テキストストリームの行区切り (adCRLF, adLF, adCR) です。"),
			comProp("Mode", "Number", "Permissions for modifying data.", "データ変更の権限です。"),
			comProp("Position", "Number", "Current position from the beginning of the stream, in bytes.", "ストリーム先頭からの現在位置 (バイト) です。"),
			comProp("Size", "Number", "Size of the stream in bytes.", "ストリームのサイズ (バイト) です。"),
			comProp("State", "Number", "Whether the stream is open or closed.", "ストリームが開いているか閉じているかを示します。"),
			comProp("Type", "Number", "Type of data in the stream (adTypeBinary or adTypeText).", "ストリームのデータの種類 (adTypeBinary または adTypeText) です。"),
			comMethod("Cancel", "Variant", "", "Cancels a pending asynchronous Open call.", "保留中の非同期 Open をキャンセルします。"),
			comMethod("Close", "Variant", "", "Closes the stream.", "ストリームを閉じます。"),
			comMethod("CopyTo", "Variant", "destStream, [numChars]", "Copies characters or bytes to another Stream.", "文字またはバイトを別の Stream へコピーします。"),
			comMethod("Flush", "Variant", "", "Sends the buffered contents to the underlying object.", "バッファーの内容を基になるオブジェクトへ送ります。"),
			comMethod("LoadFromFile", "Variant", "fileName", "Loads the contents of a file into the stream.", "ファイルの内容をストリームへ読み込みます。"),
			comMethod("Open", "Variant", "[source], [mode], [openOptions], [userName], [password]", "Opens the stream.", "ストリームを開きます。"),
			comMethod("Read", "Variant", "[numBytes]", "Reads bytes from a binary stream.", "バイナリストリームからバイトを読み取ります。"),
			comMethod("ReadText", "String", "[numChars]", "Reads characters from a text stream.", "テキストストリームから文字を読み取ります。"),
			comMethod("SaveToFile", "Variant", "fileName, [saveOptions]", "Saves the contents of the stream to a file.", "ストリームの内容をファイルへ保存します。"),
			comMethod("SetEOS", "Variant", "", "Sets the current position as the end of the stream.", "現在位置をストリームの終端にします。"),
			comMethod("SkipLine", "Variant", "", "Skips one line when reading a text stream.", "テキストストリームの読み取りで 1 行スキップします。"),
			comMethod("Write", "Variant", "buffer", "Writes binary data to the stream.", "バイナリデータをストリームへ書き込みます。"),
			comMethod("WriteText", "Variant", "data, [options]", "Writes a string to a text stream.", "文字列をテキストストリームへ書き込みます。"),
		}},
		{name: "ADODB.Record", detail: "Record", members: []vbBuiltinMember{
			comProp("ActiveConnection", "Variant", "Connection object or connection string the Record belongs to.", "Record が属する Connection オブジェクトまたは接続文字列です。"),
			comProp("Fields", "ADODB.Fields", "Collection of Field objects for the record.", "レコードの Field オブジェクトのコレクションです。"),
			comProp("Mode", "Number", "Permissions for modifying data.", "データ変更の権限です。"),
			comProp("ParentURL", "String", "Absolute URL of the parent Record.", "親 Record の絶対 URL です。"),
			comProp("Properties", "ADODB.Properties", "Provider-specific dynamic properties.", "プロバイダー固有の動的プロパティです。"),
			comProp("RecordType", "Number", "Type of the Record.", "Record の種類です。"),
			comProp("Source", "Variant", "Entity represented by the Record.", "Record が表す対象です。"),
			comProp("State", "Number", "Whether the Record is open or closed.", "Record が開いているか閉じているかを示します。"),
			comMethod("Cancel", "Variant", "", "Cancels a pending asynchronous call.", "保留中の非同期呼び出しをキャンセルします。"),
			comMethod("Close", "Variant", "", "Closes the Record.", "Record を閉じます。"),
			comMethod("CopyRecord", "String", "[source], [destination], [userName], [password], [options], [async]", "Copies the entity represented by the Record to another location.", "Record が表す対象を別の場所へコピーします。"),
			comMethod("DeleteRecord", "Variant", "[source], [async]", "Deletes the entity represented by the Record.", "Record が表す対象を削除します。"),
			comMethod("GetChildren", "ADODB.Recordset", "", "Returns a Recordset whose rows represent the children of a collection Record.", "コレクション Record の子を表す Recordset を返します。"),
			comMethod("MoveRecord", "String", "[source], [destination], [userName], [password], [options], [async]", "Moves the entity represented by the Record to another location.", "Record が表す対象を別の場所へ移動します。"),
			comMethod("Open", "Variant", "[source], [activeConnection], [mode], [createOptions], [options], [userName], [password]", "Opens an existing Record or creates a new file or directory.", "既存の Record を開くか、新しいファイルまたはディレクトリを作成します。"),
		}},
	}
}

func vbscriptScriptingStubTypes() []vbComStubType {
	fileSystemItem := func(kind, kindJA string) []vbBuiltinMember {
		return []vbBuiltinMember{
			comProp("Attributes", "Number", "Attributes of the "+kind+" (read-only, hidden, ...).", kindJA+"の属性 (読み取り専用、隠しなど) です。"),
			comProp("DateCreated", "Date", "Date and time the "+kind+" was created.", kindJA+"の作成日時です。"),
			comProp("DateLastAccessed", "Date", "Date and time the "+kind+" was last accessed.", kindJA+"の最終アクセス日時です。"),
			comProp("DateLastModified", "Date", "Date and time the "+kind+" was last modified.", kindJA+"の最終更新日時です。"),
			comProp("Drive", "Scripting.Drive", "Drive on which the "+kind+" resides.", kindJA+"があるドライブです。"),
			comProp("Name", "String", "Name of the "+kind+".", kindJA+"の名前です。"),
			comProp("ParentFolder", "Scripting.Folder", "Folder that contains the "+kind+".", kindJA+"の親フォルダーです。"),
			comProp("Path", "String", "Full path of the "+kind+".", kindJA+"のフルパスです。"),
			comProp("ShortName", "String", "Short (8.3) name of the "+kind+".", kindJA+"の短い名前 (8.3 形式) です。"),
			comProp("ShortPath", "String", "Short (8.3) path of the "+kind+".", kindJA+"の短いパス (8.3 形式) です。"),
			comProp("Size", "Number", "Size of the "+kind+" in bytes.", kindJA+"のサイズ (バイト) です。"),
			comProp("Type", "String", "Description of the "+kind+" type.", kindJA+"の種類の説明です。"),
			comMethod("Copy", "Variant", "destination, [overwrite]", "Copies the "+kind+" to another location.", kindJA+"を別の場所へコピーします。"),
			comMethod("Delete", "Variant", "[force]", "Deletes the "+kind+".", kindJA+"を削除します。"),
			comMethod("Move", "Variant", "destination", "Moves the "+kind+" to another location.", kindJA+"を別の場所へ移動します。"),
		}
	}
	return []vbComStubType{
		{name: "Scripting.FileSystemObject", detail: "FileSystemObject", members: []vbBuiltinMember{
			comProp("Drives", "Scripting.Drives", "Collection of all drives available on the machine.", "利用できるすべてのドライブのコレクションです。"),
			comMethod("BuildPath", "String", "path, name", "Appends a name to an existing path.", "既存のパスに名前を連結します。"),
			comMethod("CopyFile", "Variant", "source, destination, [overwrite]", "Copies one or more files.", "1 つ以上のファイルをコピーします。"),
			comMethod("CopyFolder", "Variant", "source, destination, [overwrite]", "Recursively copies a folder.", "フォルダーを再帰的にコピーします。"),
			comMethod("CreateFolder", "Scripting.Folder", "folderName", "Creates a folder.", "フォルダーを作成します。"),
			comMethod("CreateTextFile", "Scripting.TextStream", "fileName, [overwrite], [unicode]", "Creates a text file and returns a TextStream for writing.", "テキストファイルを作成し、書き込み用の TextStream を返します。"),
			comMethod("DeleteFile", "Variant", "fileSpec, [force]", "Deletes one or more files.", "1 つ以上のファイルを削除します。"),
			comMethod("DeleteFolder", "Variant", "folderSpec, [force]", "Deletes a folder and its contents.", "フォルダーとその内容を削除します。"),
			comMethod("DriveExists", "Boolean", "driveSpec", "Whether the drive exists.", "ドライブが存在するかどうかを返します。"),
			comMethod("FileExists", "Boolean", "fileSpec", "Whether the file exists.", "ファイルが存在するかどうかを返します。"),
			comMethod("FolderExists", "Boolean", "folderSpec", "Whether the folder exists.", "フォルダーが存在するかどうかを返します。"),
			comMethod("GetAbsolutePathName", "String", "pathSpec", "Returns the complete path for a path specification.", "パス指定から完全なパスを返します。"),
			comMethod("GetBaseName", "String", "path", "Returns the last component of a path without the extension.", "パスの最後の要素を拡張子なしで返します。"),
			comMethod("GetDrive", "Scripting.Drive", "driveSpec", "Returns the Drive object for a path.", "パスに対応する Drive オブジェクトを返します。"),
			comMethod("GetDriveName", "String", "path", "Returns the drive name of a path.", "パスのドライブ名を返します。"),
			comMethod("GetExtensionName", "String", "path", "Returns the extension of the last component of a path.", "パスの最後の要素の拡張子を返します。"),
			comMethod("GetFile", "Scripting.File", "filePath", "Returns the File object for a path.", "パスに対応する File オブジェクトを返します。"),
			comMethod("GetFileName", "String", "pathSpec", "Returns the last component of a path.", "パスの最後の要素を返します。"),
			comMethod("GetFileVersion", "String", "fileName", "Returns the version number of a file.", "ファイルのバージョン番号を返します。"),
			comMethod("GetFolder", "Scripting.Folder", "folderPath", "Returns the Folder object for a path.", "パスに対応する Folder オブジェクトを返します。"),
			comMethod("GetParentFolderName", "String", "path", "Returns the parent folder of the last component of a path.", "パスの最後の要素の親フォルダー名を返します。"),
			comMethod("GetSpecialFolder", "Scripting.Folder", "specialFolder", "Returns a special folder (0 = Windows, 1 = System, 2 = Temporary).", "特殊フォルダーを返します (0 = Windows, 1 = System, 2 = Temporary)。"),
			comMethod("GetStandardStream", "Scripting.TextStream", "standardStreamType, [unicode]", "Returns a TextStream for standard input, output, or error.", "標準入力、標準出力、または標準エラーの TextStream を返します。"),
			comMethod("GetTempName", "String", "", "Returns a randomly generated temporary file or folder name.", "ランダムな一時ファイル名またはフォルダー名を返します。"),
			comMethod("MoveFile", "Variant", "source, destination", "Moves one or more files.", "1 つ以上のファイルを移動します。"),
			comMethod("MoveFolder", "Variant", "source, destination", "Moves one or more folders.", "1 つ以上のフォルダーを移動します。"),
			comMethod("OpenTextFile", "Scripting.TextStream", "fileName, [ioMode], [create], [format]", "Opens a file and returns a TextStream (ioMode: 1 = ForReading, 2 = ForWriting, 8 = ForAppending).", "ファイルを開いて TextStream を返します (ioMode: 1 = ForReading, 2 = ForWriting, 8 = ForAppending)。"),
		}},
		{name: "Scripting.File", detail: "File", members: append(fileSystemItem("file", "ファイル"),
			comMethod("OpenAsTextStream", "Scripting.TextStream", "[ioMode], [format]", "Opens the file and returns a TextStream.", "ファイルを開いて TextStream を返します。"),
		)},
		{name: "Scripting.Folder", detail: "Folder", members: append(fileSystemItem("folder", "フォルダー"),
			comProp("Files", "Scripting.Files", "Collection of the files in the folder.", "フォルダー内のファイルのコレクションです。"),
			comProp("IsRootFolder", "Boolean", "Whether the folder is the root folder.", "ルートフォルダーかどうかを示します。"),
			comProp("SubFolders", "Scripting.Folders", "Collection of the subfolders in the folder.", "フォルダー内のサブフォルダーのコレクションです。"),
			comMethod("CreateTextFile", "Scripting.TextStream", "fileName, [overwrite], [unicode]", "Creates a text file in the folder and returns a TextStream for writing.", "フォルダー内にテキストファイルを作成し、書き込み用の TextStream を返します。"),
		)},
		{name: "Scripting.Files", detail: "Files", members: []vbBuiltinMember{
			comProp("Count", "Number", "Number of files in the collection.", "コレクション内のファイル数です。"),
			comIndexed("Item", "Scripting.File", "key", "Returns a file by name.", "名前でファイルを返します。"),
		}},
		{name: "Scripting.Folders", detail: "Folders", members: []vbBuiltinMember{
			comProp("Count", "Number", "Number of folders in the collection.", "コレクション内のフォルダー数です。"),
			comIndexed("Item", "Scripting.Folder", "key", "Returns a folder by name.", "名前でフォルダーを返します。"),
			comMethod("Add", "Scripting.Folder", "name", "Creates a subfolder.", "サブフォルダーを作成します。"),
		}},
		{name: "Scripting.Drives", detail: "Drives", members: []vbBuiltinMember{
			comProp("Count", "Number", "Number of drives in the collection.", "コレクション内のドライブ数です。"),
			comIndexed("Item", "Scripting.Drive", "key", "Returns a drive by letter.", "ドライブ文字でドライブを返します。"),
		}},
		{name: "Scripting.Drive", detail: "Drive", members: []vbBuiltinMember{
			comProp("AvailableSpace", "Number", "Space available to the user on the drive, in bytes.", "ユーザーが使えるドライブの空き容量 (バイト) です。"),
			comProp("DriveLetter", "String", "Drive letter.", "ドライブ文字です。"),
			comProp("DriveType", "Number", "Type of the drive (1 = Removable, 2 = Fixed, 3 = Network, 4 = CD-ROM, 5 = RAM disk).", "ドライブの種類 (1 = リムーバブル, 2 = 固定, 3 = ネットワーク, 4 = CD-ROM, 5 = RAM ディスク) です。"),
			comProp("FileSystem", "String", "File system of the drive (FAT, NTFS, CDFS).", "ドライブのファイルシステム (FAT, NTFS, CDFS) です。"),
			comProp("FreeSpace", "Number", "Free space on the drive, in bytes.", "ドライブの空き容量 (バイト) です。"),
			comProp("IsReady", "Boolean", "Whether the drive is ready.", "ドライブが使用可能かどうかを示します。"),
			comProp("Path", "String", "Path of the drive.", "ドライブのパスです。"),
			comProp("RootFolder", "Scripting.Folder", "Root folder of the drive.", "ドライブのルートフォルダーです。"),
			comProp("SerialNumber", "Number", "Serial number of the volume.", "ボリュームのシリアル番号です。"),
			comProp("ShareName", "String", "Network share name of the drive.", "ドライブのネットワーク共有名です。"),
			comProp("TotalSize", "Number", "Total size of the drive, in bytes.", "ドライブの全体サイズ (バイト) です。"),
			comProp("VolumeName", "String", "Volume name of the drive.", "ドライブのボリューム名です。"),
		}},
		{name: "Scripting.TextStream", detail: "TextStream", members: []vbBuiltinMember{
			comProp("AtEndOfLine", "Boolean", "True when the file pointer is immediately before the end-of-line marker.", "ファイルポインターが行末の直前にあるとき True です。"),
			comProp("AtEndOfStream", "Boolean", "True when the file pointer is at the end of the stream.", "ファイルポインターがストリームの終端にあるとき True です。"),
			comProp("Column", "Number", "Column number of the current character position.", "現在の文字位置の列番号です。"),
			comProp("Line", "Number", "Current line number.", "現在の行番号です。"),
			comMethod("Close", "Variant", "", "Closes the stream.", "ストリームを閉じます。"),
			comMethod("Read", "String", "characters", "Reads the given number of characters.", "指定した文字数を読み取ります。"),
			comMethod("ReadAll", "String", "", "Reads the entire stream.", "ストリーム全体を読み取ります。"),
			comMethod("ReadLine", "String", "", "Reads one line without the newline character.", "改行文字を除いた 1 行を読み取ります。"),
			comMethod("Skip", "Variant", "characters", "Skips the given number of characters.", "指定した文字数をスキップします。"),
			comMethod("SkipLine", "Variant", "", "Skips the next line.", "次の行をスキップします。"),
			comMethod("Write", "Variant", "text", "Writes a string to the stream.", "文字列をストリームへ書き込みます。"),
			comMethod("WriteBlankLines", "Variant", "lines", "Writes the given number of newline characters.", "指定した数の改行を書き込みます。"),
			comMethod("WriteLine", "Variant", "[text]", "Writes a string followed by a newline character.", "文字列と改行を書き込みます。"),
		}},
		{name: "Scripting.Dictionary", detail: "Dictionary", members: []vbBuiltinMember{
			comProp("CompareMode", "Number", "Comparison mode for keys (vbBinaryCompare or vbTextCompare). Set it before adding items.", "キーの比較モード (vbBinaryCompare または vbTextCompare) です。項目を追加する前に設定します。"),
			comProp("Count", "Number", "Number of items in the dictionary.", "Dictionary 内の項目数です。"),
			comIndexed("Item", "Variant", "key", "Sets or returns the item for a key. Reading a missing key adds it.", "キーに対応する項目を設定または取得します。存在しないキーを読むとキーが追加されます。"),
			comIndexed("Key", "Variant", "key", "Replaces a key with a new key.", "キーを新しいキーに置き換えます。"),
			comMethod("Add", "Variant", "key, item", "Adds a key and item pair.", "キーと項目の組を追加します。"),
			comMethod("Exists", "Boolean", "key", "Whether the key exists.", "キーが存在するかどうかを返します。"),
			comMethod("Items", "Array", "", "Returns an array of all items.", "すべての項目の配列を返します。"),
			comMethod("Keys", "Array", "", "Returns an array of all keys.", "すべてのキーの配列を返します。"),
			comMethod("Remove", "Variant", "key", "Removes a key and item pair.", "キーと項目の組を削除します。"),
			comMethod("RemoveAll", "Variant", "", "Removes all keys and items.", "すべてのキーと項目を削除します。"),
		}},
	}
}

func vbscriptXMLStubTypes() []vbComStubType {
	const nodeType = "MSXML2.IXMLDOMNode"
	const nodeListType = "MSXML2.IXMLDOMNodeList"
	httpMembers := []vbBuiltinMember{
		comProp("onreadystatechange", "Variant", "Event handler called when readyState changes.", "readyState が変わったときに呼ばれるイベントハンドラーです。"),
		comProp("readyState", "Number", "State of the request (4 = completed).", "リクエストの状態です (4 = 完了)。"),
		comProp("responseBody", "Variant", "Response body as an array of unsigned bytes.", "レスポンス本体 (バイト配列) です。"),
		comProp("responseStream", "Variant", "Response body as an IStream.", "レスポンス本体 (IStream) です。"),
		comProp("responseText", "String", "Response body as a string.", "レスポンス本体 (文字列) です。"),
		comProp("responseXML", "MSXML2.DOMDocument", "Response body parsed as an XML document.", "XML ドキュメントとして解析したレスポンス本体です。"),
		comProp("status", "Number", "HTTP status code of the response.", "レスポンスの HTTP ステータスコードです。"),
		comProp("statusText", "String", "HTTP status text of the response.", "レスポンスの HTTP ステータステキストです。"),
		comMethod("abort", "Variant", "", "Cancels the current request.", "現在のリクエストを中止します。"),
		comMethod("getAllResponseHeaders", "String", "", "Returns all response headers.", "すべてのレスポンスヘッダーを返します。"),
		comMethod("getResponseHeader", "String", "header", "Returns the value of a response header.", "レスポンスヘッダーの値を返します。"),
		comMethod("open", "Variant", "method, url, [async], [user], [password]", "Initializes a request.", "リクエストを初期化します。"),
		comMethod("send", "Variant", "[body]", "Sends the request and receives the response.", "リクエストを送信してレスポンスを受け取ります。"),
		comMethod("setRequestHeader", "Variant", "header, value", "Sets a request header.", "リクエストヘッダーを設定します。"),
	}
	serverHTTPMembers := append(append([]vbBuiltinMember{}, httpMembers...),
		comMethod("getOption", "Variant", "option", "Returns the value of a request option.", "リクエストオプションの値を返します。"),
		comMethod("setOption", "Variant", "option, value", "Sets a request option, for example to ignore certificate errors.", "リクエストオプションを設定します (証明書エラーの無視など)。"),
		comMethod("setProxy", "Variant", "proxySetting, [proxyServer], [bypassList]", "Sets the proxy configuration.", "プロキシ構成を設定します。"),
		comMethod("setProxyCredentials", "Variant", "userName, password", "Sets the credentials for the proxy server.", "プロキシサーバーの資格情報を設定します。"),
		comMethod("setTimeouts", "Variant", "resolveTimeout, connectTimeout, sendTimeout, receiveTimeout", "Sets the timeouts in milliseconds.", "タイムアウト (ミリ秒) を設定します。"),
		comMethod("waitForResponse", "Boolean", "[timeoutInSeconds]", "Waits for an asynchronous request to complete.", "非同期リクエストの完了を待ちます。"),
	)
	nodeMembers := []vbBuiltinMember{
		comProp("attributes", "MSXML2.IXMLDOMNamedNodeMap", "Attributes of the node.", "ノードの属性です。"),
		comProp("baseName", "String", "Name of the node without the namespace prefix.", "名前空間プレフィックスを除いたノード名です。"),
		comProp("childNodes", nodeListType, "Child nodes of the node.", "ノードの子ノードです。"),
		comProp("dataType", "Variant", "Data type of the node.", "ノードのデータ型です。"),
		comProp("definition", nodeType, "Definition of the node in the DTD or schema.", "DTD またはスキーマ内のノード定義です。"),
		comProp("firstChild", nodeType, "First child of the node.", "最初の子ノードです。"),
		comProp("lastChild", nodeType, "Last child of the node.", "最後の子ノードです。"),
		comProp("name", "String", "Name of the attribute.", "属性の名前です。"),
		comProp("namespaceURI", "String", "Namespace URI of the node.", "ノードの名前空間 URI です。"),
		comProp("nextSibling", nodeType, "Next sibling of the node.", "次の兄弟ノードです。"),
		comProp("nodeName", "String", "Qualified name of the node.", "ノードの修飾名です。"),
		comProp("nodeType", "Number", "Type of the node (1 = element, 2 = attribute, 3 = text, 9 = document).", "ノードの種類です (1 = 要素, 2 = 属性, 3 = テキスト, 9 = ドキュメント)。"),
		comProp("nodeTypedValue", "Variant", "Value of the node expressed in its data type.", "データ型に従って表したノードの値です。"),
		comProp("nodeTypeString", "String", "Type of the node as a string.", "ノードの種類 (文字列) です。"),
		comProp("nodeValue", "Variant", "Text associated with the node.", "ノードに関連付けられたテキストです。"),
		comProp("ownerDocument", "MSXML2.DOMDocument", "Document that contains the node.", "ノードを含むドキュメントです。"),
		comProp("parentNode", nodeType, "Parent of the node.", "親ノードです。"),
		comProp("parsed", "Boolean", "Whether the node and its descendants have been parsed.", "ノードと子孫が解析済みかどうかを示します。"),
		comProp("prefix", "String", "Namespace prefix of the node.", "ノードの名前空間プレフィックスです。"),
		comProp("previousSibling", nodeType, "Previous sibling of the node.", "前の兄弟ノードです。"),
		comProp("specified", "Boolean", "Whether the attribute value is explicitly specified.", "属性値が明示的に指定されているかどうかを示します。"),
		comProp("tagName", "String", "Name of the element.", "要素の名前です。"),
		comProp("text", "String", "Text content of the node and its descendants.", "ノードと子孫のテキスト内容です。"),
		comProp("value", "Variant", "Value of the attribute.", "属性の値です。"),
		comProp("xml", "String", "XML representation of the node and its descendants.", "ノードと子孫の XML 表現です。"),
		comMethod("appendChild", nodeType, "newChild", "Appends a node as the last child.", "ノードを最後の子として追加します。"),
		comMethod("cloneNode", nodeType, "deep", "Creates a copy of the node.", "ノードのコピーを作成します。"),
		comMethod("getAttribute", "Variant", "name", "Returns the value of an attribute.", "属性の値を返します。"),
		comMethod("getAttributeNode", nodeType, "name", "Returns an attribute node.", "属性ノードを返します。"),
		comMethod("getElementsByTagName", nodeListType, "tagName", "Returns the descendant elements with the given name.", "指定した名前の子孫要素を返します。"),
		comMethod("hasChildNodes", "Boolean", "", "Whether the node has children.", "子ノードがあるかどうかを返します。"),
		comMethod("insertBefore", nodeType, "newChild, refChild", "Inserts a child node before the reference node.", "参照ノードの前に子ノードを挿入します。"),
		comMethod("normalize", "Variant", "", "Merges adjacent text nodes.", "隣接するテキストノードを結合します。"),
		comMethod("removeAttribute", "Variant", "name", "Removes an attribute.", "属性を削除します。"),
		comMethod("removeAttributeNode", nodeType, "attribute", "Removes an attribute node.", "属性ノードを削除します。"),
		comMethod("removeChild", nodeType, "childNode", "Removes a child node.", "子ノードを削除します。"),
		comMethod("replaceChild", nodeType, "newChild, oldChild", "Replaces a child node.", "子ノードを置き換えます。"),
		comMethod("selectNodes", nodeListType, "queryString", "Returns the nodes that match an XPath expression.", "XPath 式に一致するノードを返します。"),
		comMethod("selectSingleNode", nodeType, "queryString", "Returns the first node that matches an XPath expression.", "XPath 式に一致する最初のノードを返します。"),
		comMethod("setAttribute", "Variant", "name, value", "Sets the value of an attribute.", "属性の値を設定します。"),
		comMethod("setAttributeNode", nodeType, "attribute", "Adds or replaces an attribute node.", "属性ノードを追加または置換します。"),
		comMethod("transformNode", "String", "stylesheet", "Applies an XSLT style sheet and returns the result as a string.", "XSLT スタイルシートを適用し、結果を文字列で返します。"),
		comMethod("transformNodeToObject", "Variant", "stylesheet, outputObject", "Applies an XSLT style sheet and writes the result to an object.", "XSLT スタイルシートを適用し、結果をオブジェクトへ書き込みます。"),
	}
	documentMembers := append(append([]vbBuiltinMember{}, nodeMembers...),
		comProp("async", "Boolean", "Whether loading is asynchronous. Set to False on the server.", "読み込みを非同期にするかどうかです。サーバーでは False にします。"),
		comProp("doctype", nodeType, "Document type node.", "ドキュメント型ノードです。"),
		comProp("documentElement", nodeType, "Root element of the document.", "ドキュメントのルート要素です。"),
		comProp("implementation", "Object", "Implementation object for the document.", "ドキュメントの実装オブジェクトです。"),
		comProp("namespaces", "Object", "Namespaces used in the document.", "ドキュメントで使われている名前空間です。"),
		comProp("parseError", "MSXML2.IXMLDOMParseError", "Information about the last parse error.", "直前の解析エラーの情報です。"),
		comProp("preserveWhiteSpace", "Boolean", "Whether white space is preserved.", "空白を保持するかどうかです。"),
		comProp("readyState", "Number", "State of the document (4 = completed).", "ドキュメントの状態です (4 = 完了)。"),
		comProp("resolveExternals", "Boolean", "Whether external definitions are resolved at parse time.", "解析時に外部定義を解決するかどうかです。"),
		comProp("schemas", "Variant", "Schema collection used for validation.", "検証に使うスキーマコレクションです。"),
		comProp("url", "String", "URL of the last loaded document.", "最後に読み込んだドキュメントの URL です。"),
		comProp("validateOnParse", "Boolean", "Whether the document is validated when parsed.", "解析時にドキュメントを検証するかどうかです。"),
		comMethod("abort", "Variant", "", "Aborts an asynchronous download.", "非同期ダウンロードを中止します。"),
		comMethod("createAttribute", nodeType, "name", "Creates an attribute node.", "属性ノードを作成します。"),
		comMethod("createCDATASection", nodeType, "data", "Creates a CDATA section node.", "CDATA セクションノードを作成します。"),
		comMethod("createComment", nodeType, "data", "Creates a comment node.", "コメントノードを作成します。"),
		comMethod("createDocumentFragment", nodeType, "", "Creates an empty document fragment.", "空のドキュメントフラグメントを作成します。"),
		comMethod("createElement", nodeType, "tagName", "Creates an element node.", "要素ノードを作成します。"),
		comMethod("createEntityReference", nodeType, "name", "Creates an entity reference node.", "エンティティ参照ノードを作成します。"),
		comMethod("createNode", nodeType, "type, name, namespaceURI", "Creates a node of the given type, name, and namespace.", "指定した種類、名前、名前空間のノードを作成します。"),
		comMethod("createProcessingInstruction", nodeType, "target, data", "Creates a processing instruction node.", "処理命令ノードを作成します。"),
		comMethod("createTextNode", nodeType, "data", "Creates a text node.", "テキストノードを作成します。"),
		comMethod("getProperty", "Variant", "name", "Returns a second-level property such as SelectionLanguage.", "SelectionLanguage などの二次プロパティを返します。"),
		comMethod("importNode", nodeType, "node, deep", "Clones a node from another document.", "別のドキュメントのノードを複製します。"),
		comMethod("load", "Boolean", "xmlSource", "Loads an XML document from a URL, path, or stream. Returns True on success.", "URL、パス、またはストリームから XML ドキュメントを読み込みます。成功すると True を返します。"),
		comMethod("loadXML", "Boolean", "xmlString", "Loads an XML document from a string. Returns True on success.", "文字列から XML ドキュメントを読み込みます。成功すると True を返します。"),
		comMethod("nodeFromID", nodeType, "idString", "Returns the node whose ID attribute matches.", "ID 属性が一致するノードを返します。"),
		comMethod("save", "Variant", "destination", "Saves the document to a file, stream, or another object.", "ドキュメントをファイル、ストリーム、または別のオブジェクトへ保存します。"),
		comMethod("setProperty", "Variant", "name, value", "Sets a second-level property such as SelectionLanguage or SelectionNamespaces.", "SelectionLanguage や SelectionNamespaces などの二次プロパティを設定します。"),
		comMethod("validate", "MSXML2.IXMLDOMParseError", "", "Validates the document against its DTD or schema.", "DTD またはスキーマでドキュメントを検証します。"),
	)
	return []vbComStubType{
		{name: "MSXML2.ServerXMLHTTP", detail: "ServerXMLHTTP", members: serverHTTPMembers},
		{name: "MSXML2.XMLHTTP", detail: "XMLHTTP", aliases: []string{"Microsoft.XMLHTTP"}, members: httpMembers},
		{name: "WinHttp.WinHttpRequest", detail: "WinHttpRequest", members: []vbBuiltinMember{
			comIndexed("Option", "Variant", "option", "Sets or returns a WinHTTP option.", "WinHTTP のオプションを設定または取得します。"),
			comProp("ResponseBody", "Variant", "Response body as an array of unsigned bytes.", "レスポンス本体 (バイト配列) です。"),
			comProp("ResponseStream", "Variant", "Response body as an IStream.", "レスポンス本体 (IStream) です。"),
			comProp("ResponseText", "String", "Response body as a string.", "レスポンス本体 (文字列) です。"),
			comProp("Status", "Number", "HTTP status code of the response.", "レスポンスの HTTP ステータスコードです。"),
			comProp("StatusText", "String", "HTTP status text of the response.", "レスポンスの HTTP ステータステキストです。"),
			comMethod("Abort", "Variant", "", "Aborts the current request.", "現在のリクエストを中止します。"),
			comMethod("GetAllResponseHeaders", "String", "", "Returns all response headers.", "すべてのレスポンスヘッダーを返します。"),
			comMethod("GetResponseHeader", "String", "header", "Returns the value of a response header.", "レスポンスヘッダーの値を返します。"),
			comMethod("Open", "Variant", "method, url, [async]", "Opens an HTTP connection to a resource.", "リソースへの HTTP 接続を開きます。"),
			comMethod("Send", "Variant", "[body]", "Sends the request.", "リクエストを送信します。"),
			comMethod("SetAutoLogonPolicy", "Variant", "autoLogonPolicy", "Sets the automatic logon policy.", "自動ログオンポリシーを設定します。"),
			comMethod("SetClientCertificate", "Variant", "clientCertificate", "Selects a client certificate.", "クライアント証明書を選択します。"),
			comMethod("SetCredentials", "Variant", "userName, password, flags", "Sets the credentials for the server or proxy.", "サーバーまたはプロキシの資格情報を設定します。"),
			comMethod("SetProxy", "Variant", "proxySetting, [proxyServer], [bypassList]", "Sets the proxy configuration.", "プロキシ構成を設定します。"),
			comMethod("SetRequestHeader", "Variant", "header, value", "Sets a request header.", "リクエストヘッダーを設定します。"),
			comMethod("SetTimeouts", "Variant", "resolveTimeout, connectTimeout, sendTimeout, receiveTimeout", "Sets the timeouts in milliseconds.", "タイムアウト (ミリ秒) を設定します。"),
			comMethod("WaitForResponse", "Boolean", "[timeout]", "Waits for an asynchronous request to complete.", "非同期リクエストの完了を待ちます。"),
		}},
		{name: "MSXML2.DOMDocument", detail: "DOMDocument", aliases: []string{"Microsoft.XMLDOM", "MSXML.DOMDocument", "MSXML2.FreeThreadedDOMDocument"}, members: documentMembers},
		{name: nodeType, detail: "IXMLDOMNode", aliases: []string{"MSXML2.IXMLDOMElement", "MSXML2.IXMLDOMAttribute"}, members: nodeMembers},
		{name: nodeListType, detail: "IXMLDOMNodeList", members: []vbBuiltinMember{
			comProp("length", "Number", "Number of nodes in the list.", "リスト内のノード数です。"),
			comIndexed("item", nodeType, "index", "Returns the node at a zero-based index.", "0 から始まるインデックスでノードを返します。"),
			comMethod("nextNode", nodeType, "", "Returns the next node in the list.", "リスト内の次のノードを返します。"),
			comMethod("reset", "Variant", "", "Resets the iterator.", "反復子をリセットします。"),
		}},
		{name: "MSXML2.IXMLDOMNamedNodeMap", detail: "IXMLDOMNamedNodeMap", members: []vbBuiltinMember{
			comProp("length", "Number", "Number of attributes in the map.", "マップ内の属性数です。"),
			comIndexed("item", nodeType, "index", "Returns the attribute at a zero-based index.", "0 から始まるインデックスで属性を返します。"),
			comMethod("getNamedItem", nodeType, "name", "Returns the attribute with the given name.", "指定した名前の属性を返します。"),
			comMethod("getQualifiedItem", nodeType, "baseName, namespaceURI", "Returns the attribute with the given name and namespace.", "指定した名前と名前空間の属性を返します。"),
			comMethod("nextNode", nodeType, "", "Returns the next attribute in the map.", "マップ内の次の属性を返します。"),
			comMethod("removeNamedItem", nodeType, "name", "Removes the attribute with the given name.", "指定した名前の属性を削除します。"),
			comMethod("removeQualifiedItem", nodeType, "baseName, namespaceURI", "Removes the attribute with the given name and namespace.", "指定した名前と名前空間の属性を削除します。"),
			comMethod("reset", "Variant", "", "Resets the iterator.", "反復子をリセットします。"),
			comMethod("setNamedItem", nodeType, "newItem", "Adds or replaces an attribute.", "属性を追加または置換します。"),
		}},
		{name: "MSXML2.IXMLDOMParseError", detail: "IXMLDOMParseError", members: []vbBuiltinMember{
			comProp("errorCode", "Number", "Error code of the last parse error. 0 means no error.", "直前の解析エラーのコードです。0 はエラーなしです。"),
			comProp("filepos", "Number", "Absolute file position where the error occurred.", "エラーが発生したファイル内の絶対位置です。"),
			comProp("line", "Number", "Line number that contains the error.", "エラーがある行番号です。"),
			comProp("linepos", "Number", "Character position within the line where the error occurred.", "エラーが発生した行内の文字位置です。"),
			comProp("reason", "String", "Reason for the error.", "エラーの理由です。"),
			comProp("srcText", "String", "Full text of the line that contains the error.", "エラーがある行の全文です。"),
			comProp("url", "String", "URL of the document that contains the error.", "エラーがあるドキュメントの URL です。"),
		}},
	}
}

func vbscriptMailStubTypes() []vbComStubType {
	return []vbComStubType{
		{name: "CDO.Message", detail: "Message", members: []vbBuiltinMember{
			comProp("Attachments", "Object", "Collection of the attachments of the message.", "メッセージの添付ファイルのコレクションです。"),
			comProp("AutoGenerateTextBody", "Boolean", "Whether the plain text body is generated from the HTML body.", "HTML 本文からテキスト本文を自動生成するかどうかです。"),
			comProp("BCC", "String", "Blind carbon copy recipients.", "BCC の宛先です。"),
			comProp("BodyPart", "Object", "Root body part of the message.", "メッセージのルートのボディパートです。"),
			comProp("CC", "String", "Carbon copy recipients.", "CC の宛先です。"),
			comProp("Configuration", "CDO.Configuration", "Configuration object used to send the message.", "メッセージの送信に使う Configuration オブジェクトです。"),
			comProp("DSNOptions", "Number", "Delivery status notification options.", "配信状態通知のオプションです。"),
			comProp("EnvelopeFields", "ADODB.Fields", "SMTP envelope fields of the message.", "メッセージの SMTP エンベロープのフィールドです。"),
			comProp("Fields", "ADODB.Fields", "Header fields of the message.", "メッセージのヘッダーフィールドです。"),
			comProp("FollowUpTo", "String", "Newsgroups to which responses are posted.", "返信を投稿するニュースグループです。"),
			comProp("From", "String", "Sender address shown to recipients.", "受信者に表示される差出人アドレスです。"),
			comProp("HTMLBody", "String", "HTML body of the message.", "メッセージの HTML 本文です。"),
			comProp("HTMLBodyPart", "Object", "Body part that contains the HTML body.", "HTML 本文を持つボディパートです。"),
			comProp("Keywords", "String", "Keywords of the message.", "メッセージのキーワードです。"),
			comProp("MDNRequested", "Boolean", "Whether a message disposition notification is requested.", "開封確認を要求するかどうかです。"),
			comProp("MIMEFormatted", "Boolean", "Whether the message is formatted with MIME.", "メッセージを MIME 形式にするかどうかです。"),
			comProp("Newsgroups", "String", "Newsgroup recipients.", "宛先のニュースグループです。"),
			comProp("Organization", "String", "Organization of the sender.", "差出人の組織です。"),
			comProp("ReceivedTime", "Date", "Date and time the message was received.", "メッセージの受信日時です。"),
			comProp("ReplyTo", "String", "Addresses to which replies are sent.", "返信先のアドレスです。"),
			comProp("Sender", "String", "Address of the actual sender.", "実際の送信者のアドレスです。"),
			comProp("SentOn", "Date", "Date and time the message was sent.", "メッセージの送信日時です。"),
			comProp("Subject", "String", "Subject of the message.", "メッセージの件名です。"),
			comProp("TextBody", "String", "Plain text body of the message.", "メッセージのテキスト本文です。"),
			comProp("TextBodyPart", "Object", "Body part that contains the plain text body.", "テキスト本文を持つボディパートです。"),
			comProp("To", "String", "Primary recipients.", "宛先です。"),
			comMethod("AddAttachment", "Object", "url, [userName], [password]", "Adds an attachment and returns its body part.", "添付ファイルを追加し、そのボディパートを返します。"),
			comMethod("AddRelatedBodyPart", "Object", "url, reference, referenceType, [userName], [password]", "Adds a body part referenced from the HTML body, such as an inline image.", "HTML 本文から参照するボディパート (インライン画像など) を追加します。"),
			comMethod("CreateMHTMLBody", "Variant", "url, [flags], [userName], [password]", "Converts a web page into the MHTML body of the message.", "Web ページをメッセージの MHTML 本文に変換します。"),
			comMethod("Forward", "CDO.Message", "", "Creates a message used to forward this message.", "このメッセージを転送するためのメッセージを作成します。"),
			comMethod("GetInterface", "Object", "interfaceName", "Returns the named interface on the object.", "指定した名前のインターフェイスを返します。"),
			comMethod("GetStream", "ADODB.Stream", "", "Returns a Stream that contains the serialized message.", "シリアル化したメッセージを持つ Stream を返します。"),
			comMethod("Post", "Variant", "", "Posts the message to newsgroups.", "メッセージをニュースグループへ投稿します。"),
			comMethod("PostReply", "CDO.Message", "", "Creates a message used to post a reply.", "返信を投稿するためのメッセージを作成します。"),
			comMethod("Reply", "CDO.Message", "", "Creates a message used to reply to the sender.", "差出人へ返信するためのメッセージを作成します。"),
			comMethod("ReplyAll", "CDO.Message", "", "Creates a message used to reply to all recipients.", "全員へ返信するためのメッセージを作成します。"),
			comMethod("Send", "Variant", "", "Sends the message.", "メッセージを送信します。"),
		}},
		{name: "CDO.Configuration", detail: "Configuration", members: []vbBuiltinMember{
			comProp("Fields", "ADODB.Fields", "Configuration fields, for example http://schemas.microsoft.com/cdo/configuration/smtpserver. Call Fields.Update after changing them.", "構成フィールドです (例: http://schemas.microsoft.com/cdo/configuration/smtpserver)。変更後に Fields.Update を呼びます。"),
			comMethod("GetInterface", "Object", "interfaceName", "Returns the named interface on the object.", "指定した名前のインターフェイスを返します。"),
			comMethod("Load", "Variant", "loadFrom, [url]", "Loads the default configuration.", "既定の構成を読み込みます。"),
		}},
		{name: "CDONTS.NewMail", detail: "NewMail", members: []vbBuiltinMember{
			comProp("Bcc", "String", "Blind carbon copy recipients.", "BCC の宛先です。"),
			comProp("Body", "String", "Body of the message.", "メッセージの本文です。"),
			comProp("BodyFormat", "Number", "Format of the body (0 = HTML, 1 = plain text).", "本文の形式です (0 = HTML, 1 = テキスト)。"),
			comProp("Cc", "String", "Carbon copy recipients.", "CC の宛先です。"),
			comProp("ContentBase", "String", "Base URL for URLs in the body.", "本文内の URL の基準 URL です。"),
			comProp("ContentLocation", "String", "Location of the content in the body.", "本文内のコンテンツの場所です。"),
			comProp("From", "String", "Sender address.", "差出人アドレスです。"),
			comProp("Importance", "Number", "Importance of the message (0 = low, 1 = normal, 2 = high).", "メッセージの重要度です (0 = 低, 1 = 標準, 2 = 高)。"),
			comProp("MailFormat", "Number", "Encoding of the message (0 = MIME, 1 = plain text).", "メッセージのエンコードです (0 = MIME, 1 = テキスト)。"),
			comProp("Subject", "String", "Subject of the message.", "メッセージの件名です。"),
			comProp("To", "String", "Primary recipients.", "宛先です。"),
			comIndexed("Value", "String", "header", "Adds a header to the message.", "メッセージにヘッダーを追加します。"),
			comProp("Version", "String", "Version of the CDONTS library.", "CDONTS ライブラリのバージョンです。"),
			comMethod("AttachFile", "Variant", "source, [fileName], [encodingMethod]", "Attaches a file to the message.", "メッセージにファイルを添付します。"),
			comMethod("AttachURL", "Variant", "source, contentLocation, [contentBase], [encodingMethod]", "Attaches a file and associates a URL with it.", "ファイルを添付し、URL を関連付けます。"),
			comMethod("Send", "Variant", "[from], [to], [subject], [body], [importance]", "Sends the message.", "メッセージを送信します。"),
			comMethod("SetLocaleIDs", "Variant", "codePageID", "Sets the code page used by the message.", "メッセージで使うコードページを設定します。"),
		}},
	}
}

func vbscriptWSHStubTypes() []vbComStubType {
	return []vbComStubType{
		{name: "WScript.Shell", detail: "WshShell", members: []vbBuiltinMember{
			comProp("CurrentDirectory", "String", "Current working directory.", "現在の作業ディレクトリです。"),
			comIndexed("Environment", "Object", "[type]", "Returns the environment variable collection (\"System\", \"User\", \"Volatile\", or \"Process\").", "環境変数のコレクションを返します (\"System\", \"User\", \"Volatile\", \"Process\")。"),
			comIndexed("SpecialFolders", "Variant", "[name]", "Returns the path of a special folder such as \"Desktop\".", "\"Desktop\" などの特殊フォルダーのパスを返します。"),
			comMethod("AppActivate", "Boolean", "title, [wait]", "Activates an application window.", "アプリケーションウィンドウをアクティブにします。"),
			comMethod("CreateShortcut", "Object", "pathName", "Creates or opens a shortcut.", "ショートカットを作成または開きます。"),
			comMethod("Exec", "WScript.ScriptExec", "command", "Runs a command in a child shell with access to its standard streams.", "子シェルでコマンドを実行し、標準ストリームへアクセスできるようにします。"),
			comMethod("ExpandEnvironmentStrings", "String", "text", "Expands environment variables in a string.", "文字列内の環境変数を展開します。"),
			comMethod("LogEvent", "Boolean", "type, message, [target]", "Adds an entry to the event log.", "イベントログへエントリを追加します。"),
			comMethod("Popup", "Number", "text, [secondsToWait], [title], [type]", "Displays a message box and returns the clicked button.", "メッセージボックスを表示し、押されたボタンを返します。"),
			comMethod("RegDelete", "Variant", "name", "Deletes a registry key or value.", "レジストリのキーまたは値を削除します。"),
			comMethod("RegRead", "Variant", "name", "Reads a registry key or value.", "レジストリのキーまたは値を読み取ります。"),
			comMethod("RegWrite", "Variant", "name, value, [type]", "Writes a registry key or value.", "レジストリのキーまたは値を書き込みます。"),
			comMethod("Run", "Number", "command, [windowStyle], [waitOnReturn]", "Runs a program in a new process and returns its exit code when waiting.", "新しいプロセスでプログラムを実行します。待機した場合は終了コードを返します。"),
			comMethod("SendKeys", "Variant", "keys, [wait]", "Sends keystrokes to the active window.", "アクティブウィンドウへキー入力を送ります。"),
		}},
		{name: "WScript.ScriptExec", detail: "WshScriptExec", members: []vbBuiltinMember{
			comProp("ExitCode", "Number", "Exit code of the process.", "プロセスの終了コードです。"),
			comProp("ProcessID", "Number", "Process ID of the process.", "プロセスのプロセス ID です。"),
			comProp("Status", "Number", "Status of the process (0 = running, 1 = done).", "プロセスの状態です (0 = 実行中, 1 = 完了)。"),
			comProp("StdErr", "Scripting.TextStream", "Standard error stream of the process.", "プロセスの標準エラーストリームです。"),
			comProp("StdIn", "Scripting.TextStream", "Standard input stream of the process.", "プロセスの標準入力ストリームです。"),
			comProp("StdOut", "Scripting.TextStream", "Standard output stream of the process.", "プロセスの標準出力ストリームです。"),
			comMethod("Terminate", "Variant", "", "Terminates the process.", "プロセスを終了します。"),
		}},
		{name: "WScript.Network", detail: "WshNetwork", members: []vbBuiltinMember{
			comProp("ComputerName", "String", "Name of the computer.", "コンピューター名です。"),
			comProp("UserDomain", "String", "Domain of the current user.", "現在のユーザーのドメインです。"),
			comProp("UserName", "String", "Name of the current user.", "現在のユーザー名です。"),
			comMethod("AddPrinterConnection", "Variant", "localName, remoteName, [updateProfile], [user], [password]", "Adds a remote MS-DOS based printer connection.", "MS-DOS ベースのリモートプリンター接続を追加します。"),
			comMethod("AddWindowsPrinterConnection", "Variant", "printerPath, [driverName], [port]", "Adds a Windows printer connection.", "Windows プリンター接続を追加します。"),
			comMethod("EnumNetworkDrives", "Object", "", "Returns the current network drive mappings.", "現在のネットワークドライブの割り当てを返します。"),
			comMethod("EnumPrinterConnections", "Object", "", "Returns the current network printer mappings.", "現在のネットワークプリンターの割り当てを返します。"),
			comMethod("MapNetworkDrive", "Variant", "localName, remoteName, [updateProfile], [user], [password]", "Maps a network share to a drive letter.", "ネットワーク共有をドライブ文字に割り当てます。"),
			comMethod("RemoveNetworkDrive", "Variant", "name, [force], [updateProfile]", "Removes a network drive mapping.", "ネットワークドライブの割り当てを解除します。"),
			comMethod("RemovePrinterConnection", "Variant", "name, [force], [updateProfile]", "Removes a network printer connection.", "ネットワークプリンター接続を削除します。"),
			comMethod("SetDefaultPrinter", "Variant", "printerName", "Sets the default printer.", "既定のプリンターを設定します。"),
		}},
	}
}

// vbscriptBuiltinMemberChainType resolves "owner.Member(args).Member" through
// the built-in catalog when the owner has a built-in type. resolved reports
// that the whole expression was understood, even when the result is Variant.
func vbscriptBuiltinMemberChainType(value string, ownerType func(name string) string) (typeName string, resolved bool) {
	value = strings.TrimSpace(value)
	cursor := readVBIdentifier(value, 0)
	if cursor == 0 || cursor >= len(value) {
		return "", false
	}
	candidates := vbscriptConcreteTypeNames(ownerType(value[:cursor]))
	if len(candidates) != 1 {
		return "", false
	}
	typeName = candidates[0]
	skipSpace := func() {
		for cursor < len(value) && isVBWhitespace(value[cursor]) {
			cursor++
		}
	}
	for {
		members := vbscriptBuiltinTypeMembers(typeName)
		if len(members) == 0 {
			return "", false
		}
		skipSpace()
		if cursor >= len(value) || value[cursor] != '.' {
			return "", false
		}
		cursor++
		skipSpace()
		end := readVBIdentifier(value, cursor)
		if end == cursor {
			return "", false
		}
		member, ok := members[strings.ToLower(value[cursor:end])]
		if !ok {
			return "", false
		}
		cursor = end
		typeName = member.TypeName
		skipSpace()
		if cursor < len(value) && value[cursor] == '(' {
			close := vbMatchingCloseParen(value, cursor)
			if close < 0 {
				return "", false
			}
			cursor = close + 1
			if member.Signature == "" {
				// Arguments on a plain property index its default Item member.
				if item, ok := vbscriptBuiltinTypeMembers(typeName)["item"]; ok {
					typeName = item.TypeName
				} else {
					typeName = "Variant"
				}
			}
			skipSpace()
		}
		if cursor >= len(value) {
			return typeName, true
		}
	}
}

// vbReceiverExpressionStart walks backwards from the member-access dot at dot
// and returns where its receiver expression starts on the same line. start is
// -1 when there is no receiver (a With-block ".Member") or it is not a plain
// identifier/call chain. simple reports a bare identifier receiver.
func vbReceiverExpressionStart(text string, dot int) (start int, simple bool) {
	if dot <= 0 || dot > len(text) {
		return -1, false
	}
	isSpace := func(index int) bool { return text[index] == ' ' || text[index] == '\t' }
	cursor := dot
	segments := 0
	calls := 0
	for {
		for cursor > 0 && isSpace(cursor-1) {
			cursor--
		}
		if cursor > 0 && text[cursor-1] == ')' {
			open := vbMatchingOpenParen(text, cursor-1)
			if open < 0 {
				return -1, false
			}
			cursor = open
			calls++
			for cursor > 0 && isSpace(cursor-1) {
				cursor--
			}
		}
		end := cursor
		for cursor > 0 && isVBIdentifierByte(text[cursor-1]) {
			cursor--
		}
		if cursor == end {
			return -1, false
		}
		segments++
		previous := cursor
		for previous > 0 && isSpace(previous-1) {
			previous--
		}
		if previous == 0 || text[previous-1] != '.' {
			return cursor, segments == 1 && calls == 0
		}
		cursor = previous - 1
	}
}

// vbMatchingOpenParen returns the index of the parenthesis opened for the one
// closing at close, scanning backwards within the line and skipping string
// literals, or -1 when it is unbalanced.
func vbMatchingOpenParen(text string, close int) int {
	depth := 0
	inString := false
	for index := close; index >= 0; index-- {
		switch character := text[index]; {
		case character == '\n' || character == '\r':
			return -1
		case character == '"':
			inString = !inString
		case inString:
		case character == ')':
			depth++
		case character == '(':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

// vbMemberDotBefore returns the offset of the member-access dot that precedes
// the member name starting at nameStart, or -1.
func vbMemberDotBefore(text string, nameStart int) int {
	cursor := nameStart
	for cursor > 0 && (text[cursor-1] == ' ' || text[cursor-1] == '\t') {
		cursor--
	}
	if cursor == 0 || text[cursor-1] != '.' {
		return -1
	}
	return cursor - 1
}

// vbscriptChainReceiverTypesContext resolves the type of a multi-segment
// receiver such as "rs.Fields(0)" ending at dot. handled is false for bare
// identifiers and With-block members, which the single-owner paths cover.
func (s *Server) vbscriptChainReceiverTypesContext(ctx context.Context, parsed *core.ParsedDocument, dot int, offset int) (typeNames []string, handled bool, complete bool) {
	start, simple := vbReceiverExpressionStart(parsed.Text, dot)
	if start < 0 || simple {
		return nil, false, true
	}
	var incomplete bool
	ownerType := func(name string) string {
		owners, ok := s.vbscriptKnownTypesForNameAtContext(ctx, parsed, name, offset)
		if !ok {
			incomplete = true
			return ""
		}
		return strings.Join(owners, " | ")
	}
	typeName, resolved := vbscriptBuiltinMemberChainType(parsed.Text[start:dot], ownerType)
	if incomplete || ctx.Err() != nil {
		return nil, true, false
	}
	if !resolved {
		return nil, true, true
	}
	return vbscriptConcreteTypeNames(typeName), true, true
}

// vbMatchingCloseParen returns the index of the parenthesis closing the one at
// open, skipping string literals, or -1 when it is unbalanced.
func vbMatchingCloseParen(text string, open int) int {
	depth := 0
	for index := open; index < len(text); index++ {
		switch text[index] {
		case '"':
			for index++; index < len(text) && text[index] != '"'; index++ {
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}
