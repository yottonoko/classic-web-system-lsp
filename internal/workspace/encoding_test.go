package workspace

import "testing"

func TestDecodeLegacyAutoKeepsUTF8(t *testing.T) {
	got, err := DecodeLegacy([]byte("日本語"), "auto")
	if err != nil {
		t.Fatalf("DecodeLegacy returned error: %v", err)
	}
	if got != "日本語" {
		t.Fatalf("DecodeLegacy = %q, want 日本語", got)
	}
}

func TestDecodeLegacyAutoFallsBackToShiftJIS(t *testing.T) {
	got, err := DecodeLegacy([]byte{0x8e, 0x9f}, "auto")
	if err != nil {
		t.Fatalf("DecodeLegacy returned error: %v", err)
	}
	if got != "次" {
		t.Fatalf("DecodeLegacy = %q, want 次", got)
	}
}

func TestDecodeLegacyUTF8DoesNotAutoDetectShiftJIS(t *testing.T) {
	got, err := DecodeLegacy([]byte{0x8e, 0x9f}, "utf8")
	if err != nil {
		t.Fatalf("DecodeLegacy returned error: %v", err)
	}
	if got == "次" {
		t.Fatalf("utf8 mode should not decode Shift_JIS content")
	}
}
