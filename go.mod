module github.com/yottonoko/classic-web-system-lsp

go 1.26.6

require (
	github.com/fxamacker/cbor/v2 v2.9.2
	github.com/microsoft/typescript-go v0.0.0
	github.com/xuri/excelize/v2 v2.11.0
	github.com/yottonoko/js-beautify-go v0.0.0
	github.com/yottonoko/vscode-css-languageservice-go v0.0.0
	github.com/yottonoko/vscode-html-languageservice-go v0.0.0
	go.etcd.io/bbolt v1.5.0
	golang.org/x/text v0.41.0
)

require (
	github.com/go-json-experiment/json v0.0.0-20260623181947-01eb4420fa68 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/richardlehane/mscfb v1.0.7 // indirect
	github.com/richardlehane/msoleps v1.0.6 // indirect
	github.com/tiendc/go-deepcopy v1.7.2 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xuri/efp v0.0.1 // indirect
	github.com/xuri/nfp v0.0.2-0.20250530014748-2ddeb826f9a9 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

replace github.com/yottonoko/js-beautify-go => ./third_party/js-beautify-go

replace github.com/yottonoko/vscode-css-languageservice-go => ./third_party/vscode-css-languageservice-go

replace github.com/yottonoko/vscode-html-languageservice-go => ./third_party/vscode-html-languageservice-go

replace github.com/microsoft/typescript-go => ./third_party/typescript-go
