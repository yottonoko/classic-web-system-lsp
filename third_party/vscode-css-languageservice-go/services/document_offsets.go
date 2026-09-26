package services

import "github.com/yottonoko/vscode-css-languageservice-go/lsp"

func positionAtByteOffset(document *lsp.TextDocument, byteOffset int) lsp.Position {
	return document.PositionAtByteOffset(byteOffset)
}

func rangeFromByteOffsets(document *lsp.TextDocument, start, end int) lsp.Range {
	return lsp.Range{
		Start: positionAtByteOffset(document, start),
		End:   positionAtByteOffset(document, end),
	}
}

func utf16OffsetAtByteOffset(document *lsp.TextDocument, byteOffset int) int {
	return document.UTF16OffsetAtByteOffset(byteOffset)
}

func byteOffsetAtPosition(document *lsp.TextDocument, position lsp.Position) int {
	return document.ByteOffsetAt(position)
}
