package unpackers

import (
	"encoding/base64"
	"net/url"
	"testing"
)

func TestURLencoded(t *testing.T) {
	u := URLencoded{}
	if !u.Detect("var%20a=b") {
		t.Fatal("expected urlencoded detection")
	}
	if got := u.Unpack("var%20a=b%2b1"); got != "var a=b+1" {
		t.Fatalf("Unpack() = %q", got)
	}
}

func TestJavascriptObfuscator(t *testing.T) {
	j := JavascriptObfuscator{}
	source := `var _0x1234=["\x61","b"];alert(_0x1234[0]+_0x1234[1]);`
	if !j.Detect(source) {
		t.Fatal("expected javascript obfuscator detection")
	}
	if got := j.Unpack(source); got != "alert('a'+'b');" {
		t.Fatalf("Unpack() = %q", got)
	}
}

func TestPacker(t *testing.T) {
	p := Packer{}
	source := "eval(function(p,a,c,k,e,r){e=String;if(!''.replace(/^/,String)){while(c--)r[c]=k[c]||c;k=[function(e){return r[e]}];e=function(){return'\\\\w+'};c=1};while(c--)if(k[c])p=p.replace(new RegExp('\\\\b'+e(c)+'\\\\b','g'),k[c]);return p}('0 2=1',3,3,'var||a'.split('|'),0,{}))"
	if !p.Detect(source) {
		t.Fatal("expected packer detection")
	}
	if got := p.Unpack(source); got != "var a=1" {
		t.Fatalf("Unpack() = %q", got)
	}
}

func TestMyObfuscate(t *testing.T) {
	m := MyObfuscate{}
	payload := "var _escape='<script>alert(1);</script>'"
	encoded := base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(payload)))
	source := "var OO0=" + myObfuscateSignature + ";var _0OO='" + reverseASCII(encoded) + "';eval(_1OO(O0I(_0OO)));"
	if !m.Detect(source) {
		t.Fatal("expected myobfuscate detection")
	}
	if got := m.Unpack(source); got != myObfuscateWarning+"alert(1);" {
		t.Fatalf("Unpack() = %q", got)
	}
}
