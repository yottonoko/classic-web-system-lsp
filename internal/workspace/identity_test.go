package workspace

import (
	"net/url"
	"strings"
	"testing"
)

// fileIdentityKeyFromURISlow mirrors the pre-optimization file URI parsing so
// the fast path can be checked for agreement.
func fileIdentityKeyFromURISlow(uri string) string {
	if len(uri) < len("file://") || !strings.EqualFold(uri[:len("file://")], "file://") {
		return SourceURIIdentityKey(uri)
	}
	parsed, err := url.Parse(uri)
	if err != nil || strings.ToLower(parsed.Scheme) != "file" {
		return SourceURIIdentityKey(uri)
	}
	pathname, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return SourceURIIdentityKey(uri)
	}
	if isWindowsDrivePath(parsed.Host) {
		return FileIdentityKeyFromFileName(parsed.Host + pathname)
	}
	if parsed.Host != "" {
		return FileIdentityKeyFromFileName("//" + parsed.Host + pathname)
	}
	pathname = strings.ReplaceAll(pathname, "\\", "/")
	return FileIdentityKeyFromFileName(stripLeadingDriveSlash(pathname))
}

func TestSourceURIIdentityKey(t *testing.T) {
	cases := []struct {
		name   string
		uri    string
		key    string
		sameAs string
	}{
		{
			name:   "normalizes Windows drive file URI case and escaping",
			uri:    "file:///C:/Site/Default.asp",
			key:    "c:/site/default.asp",
			sameAs: "file:///c%3A/site/default.asp",
		},
		{
			name:   "accepts mixed-case file URI schemes",
			uri:    "FiLe:///C:/Site/Default.asp",
			key:    "c:/site/default.asp",
			sameAs: "file:///c:/site/default.asp",
		},
		{
			name:   "normalizes UNC host and path case",
			uri:    "file://Server/Share/Folder/Default.asp",
			key:    "//server/share/folder/default.asp",
			sameAs: "file://server/share/folder/default.asp",
		},
		{
			name:   "matches four-slash UNC URI with authority form",
			uri:    "file:////Server/Share/Folder/Default.asp",
			key:    "//server/share/folder/default.asp",
			sameAs: "file://server/share/folder/default.asp",
		},
		{
			name:   "decodes percent escaped POSIX path segments without folding case",
			uri:    "file:///Users/yottonoko/My%20Site/Default.asp",
			key:    "/Users/yottonoko/My Site/Default.asp",
			sameAs: "file:///Users/yottonoko/My Site/Default.asp",
		},
		{
			name:   "preserves query and hash as part of the identity",
			uri:    "file:///C:/Site/Default.asp?view=1#runtime-global",
			key:    "c:/site/default.asp?view=1#runtime-global",
			sameAs: "file:///c:/site/default.asp?view=1#runtime-global",
		},
		{
			name:   "leaves non-file URIs unchanged",
			uri:    "untitled:Untitled-1",
			key:    "untitled:Untitled-1",
			sameAs: "untitled:Untitled-1",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := SourceURIIdentityKey(tt.uri); got != tt.key {
				t.Fatalf("identity key = %q, want %q", got, tt.key)
			}
			if !SameSourceURI(tt.uri, tt.sameAs) {
				t.Fatalf("expected %q and %q to have same source identity", tt.uri, tt.sameAs)
			}
		})
	}
}

func TestSourceURIIdentityKeepsQueryAndHashDistinct(t *testing.T) {
	if SameSourceURI("file:///C:/site/default.asp?view=1", "file:///C:/site/default.asp") {
		t.Fatalf("query should be part of source URI identity")
	}
	if SameSourceURI("file:///C:/site/default.asp#runtime-global", "file:///C:/site/default.asp") {
		t.Fatalf("hash should be part of source URI identity")
	}
}

func TestFileIdentityNormalizesWindowsURI(t *testing.T) {
	left := FileIdentityKeyFromURI("file:///C:/site/default.asp")
	right := FileIdentityKeyFromURI("file:///c:/site/default.asp")
	if left != "c:/site/default.asp" {
		t.Fatalf("Windows file identity = %q, want c:/site/default.asp", left)
	}
	if left != right {
		t.Fatalf("expected Windows file URI drive case to match: %q != %q", left, right)
	}
	escaped := FileIdentityKeyFromURI("file:///c%3A/site/default.asp")
	if left != escaped {
		t.Fatalf("expected Windows file URI escaping to match: %q != %q", left, escaped)
	}
	legacy := FileIdentityKeyFromURI("file://C:/site/default.asp")
	if left != legacy {
		t.Fatalf("expected legacy Windows file URI host form to match: %q != %q", left, legacy)
	}
}

func TestFileIdentitySimpleURIMatchesParsedResult(t *testing.T) {
	uris := []string{
		"file:///site/default.asp",
		"file:///C:/Site/Default.asp",
		"file:///c:/site/default.asp",
		"FiLe:///C:/Site/Default.asp",
		"file:///Users/yottonoko/My Site/Default.asp",
		"file:///C:/Site/Back\\Slash.asp",
		"file:///",
		"file:///c%3A/site/default.asp",
		"file://C:/site/default.asp",
		"file://Server/Share/Folder/Default.asp",
		"file:///C:/Site/Default.asp?view=1#runtime-global",
		"untitled:Untitled-1",
	}
	for _, uri := range uris {
		uri := uri
		t.Run(uri, func(t *testing.T) {
			slow := fileIdentityKeyFromURISlow(uri)
			if got := FileIdentityKeyFromURI(uri); got != slow {
				t.Fatalf("FileIdentityKeyFromURI(%q) = %q, want slow-path %q", uri, got, slow)
			}
		})
	}
}

func BenchmarkFileIdentityKeyFromURI(b *testing.B) {
	uris := []string{
		"file:///Users/yottonoko/Site/Default.asp",
		"file:///C:/Site/Include/Header.asp",
		"file:///Users/yottonoko/My Site/With Spaces.asp",
	}
	b.Run("fast", func(b *testing.B) {
		for b.Loop() {
			for _, uri := range uris {
				if FileIdentityKeyFromURI(uri) == "" {
					b.Fatal("empty identity")
				}
			}
		}
	})
	b.Run("slow", func(b *testing.B) {
		for b.Loop() {
			for _, uri := range uris {
				if fileIdentityKeyFromURISlow(uri) == "" {
					b.Fatal("empty identity")
				}
			}
		}
	})
}

func TestFileIdentityNormalizesWindowsFileNamesAcrossHosts(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     string
	}{
		{name: "drive path with backslashes", fileName: `C:\Site\Default.asp`, want: "c:/site/default.asp"},
		{name: "drive path with forward slashes", fileName: "C:/Site/./Default.asp", want: "c:/site/default.asp"},
		{name: "UNC path", fileName: `\\Server\Share\Folder\Default.asp`, want: "//server/share/folder/default.asp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := FileIdentityKeyFromFileName(test.fileName); got != test.want {
				t.Fatalf("FileIdentityKeyFromFileName(%q) = %q, want %q", test.fileName, got, test.want)
			}
		})
	}
}
