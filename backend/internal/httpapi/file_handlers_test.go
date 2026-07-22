package httpapi

import "testing"

func TestSafeUploadFileName(t *testing.T) {
	if got := safeUploadFileName(`C:\fakepath\cab-card.pdf`, "cab card"); got != "cab-card.pdf" {
		t.Fatalf("safeUploadFileName() = %q", got)
	}
	if got := safeUploadFileName(" ", "cab card"); got != "cab-card" {
		t.Fatalf("blank safeUploadFileName() = %q", got)
	}
}

func TestDetectUploadContentTypePrefersFileSignature(t *testing.T) {
	pdf := []byte("%PDF-1.7\n")
	if got := detectUploadContentType("image/png", pdf); got != "application/pdf" {
		t.Fatalf("content type = %q, want application/pdf", got)
	}
}
