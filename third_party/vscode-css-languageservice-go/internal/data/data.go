package data

import _ "embed"

//go:embed web_custom_data.json
var webCustomData []byte

func WebCustomData() []byte {
	return webCustomData
}
