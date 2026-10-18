package httpapi

import "testing"

func TestBriefReferenceImageMIME(t *testing.T) {
	for _, test := range []struct {
		ext  string
		want string
	}{
		{ext: ".png", want: "image/png"},
		{ext: ".jpg", want: "image/jpeg"},
		{ext: ".jpeg", want: "image/jpeg"},
		{ext: ".webp", want: "image/webp"},
	} {
		got, ok := briefReferenceImageMIME(test.ext)
		if !ok || got != test.want {
			t.Fatalf("briefReferenceImageMIME(%q) = %q, %v; want %q, true", test.ext, got, ok, test.want)
		}
	}
	if _, ok := briefReferenceImageMIME(".pdf"); ok {
		t.Fatal("PDF must not be treated as an image reference in the research upload")
	}
}
