package docx

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestParseTextAndImages(t *testing.T) {
	var buffer bytes.Buffer; writer := zip.NewWriter(&buffer)
	textFile, _ := writer.Create("word/document.xml"); _, _ = textFile.Write([]byte(`<w:document xmlns:w="x"><w:body><w:p><w:r><w:t>产品包装需求</w:t></w:r></w:p></w:body></w:document>`))
	imageFile, _ := writer.Create("word/media/image1.png"); _, _ = imageFile.Write([]byte{1, 2, 3}); _ = writer.Close()
	document, err := Parse(buffer.Bytes()); if err != nil { t.Fatal(err) }
	if document.Text != "产品包装需求" { t.Fatalf("text=%q", document.Text) }
	if len(document.Images) != 1 || document.Images[0].MIME != "image/png" { t.Fatalf("images=%#v", document.Images) }
}
