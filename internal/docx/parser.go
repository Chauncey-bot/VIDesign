package docx

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
)

type Image struct {
	Name    string
	MIME    string
	DataURL string
}
type Document struct {
	Text   string
	Images []Image
}

func Parse(data []byte) (Document, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Document{}, fmt.Errorf("open docx: %w", err)
	}
	var result Document
	for _, file := range reader.File {
		if file.Name == "word/document.xml" {
			text, err := parseXML(file)
			if err != nil {
				return Document{}, err
			}
			result.Text = text
		}
		if strings.HasPrefix(file.Name, "word/media/") && !file.FileInfo().IsDir() {
			image, err := readImage(file)
			if err != nil {
				return Document{}, err
			}
			result.Images = append(result.Images, image)
		}
	}
	if strings.TrimSpace(result.Text) == "" && len(result.Images) == 0 {
		return Document{}, fmt.Errorf("docx contains no readable content")
	}
	return result, nil
}

func parseXML(file *zip.File) (string, error) {
	stream, err := file.Open()
	if err != nil {
		return "", err
	}
	defer stream.Close()
	decoder := xml.NewDecoder(stream)
	var builder strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch value := token.(type) {
		case xml.CharData:
			builder.WriteString(string(value))
		case xml.EndElement:
			if value.Name.Local == "p" {
				builder.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(builder.String()), nil
}

func readImage(file *zip.File) (Image, error) {
	stream, err := file.Open()
	if err != nil {
		return Image{}, err
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, 12<<20))
	if err != nil {
		return Image{}, err
	}
	ext := filepath.Ext(file.Name)
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return Image{Name: filepath.Base(file.Name), MIME: mimeType, DataURL: "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)}, nil
}
