package lspserver

import "testing"

func TestEmbeddedCompletionFallbackLocalizationMatchesLegacyMessages(t *testing.T) {
	tests := []struct {
		kind          string
		locale        string
		wantDetail    string
		wantDocuments string
	}{
		{kind: "html", locale: "en", wantDetail: "HTML completion", wantDocuments: "Completion provided by vscode-html-languageservice."},
		{kind: "css", locale: "en", wantDetail: "CSS completion", wantDocuments: "Completion provided by vscode-css-languageservice."},
		{kind: "html", locale: "ja", wantDetail: "HTML 補完", wantDocuments: "vscode-html-languageservice による補完です。"},
		{kind: "css", locale: "ja", wantDetail: "CSS 補完", wantDocuments: "vscode-css-languageservice による補完です。"},
	}
	for _, test := range tests {
		t.Run(test.kind+"-"+test.locale, func(t *testing.T) {
			if got := embeddedCompletionDetail(test.kind, test.locale); got != test.wantDetail {
				t.Fatalf("detail = %q, want %q", got, test.wantDetail)
			}
			if got := embeddedCompletionDocumentation(test.kind, test.locale); got != test.wantDocuments {
				t.Fatalf("documentation = %q, want %q", got, test.wantDocuments)
			}
		})
	}
}
