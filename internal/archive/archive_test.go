package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func buildTarGz(entries map[string]string) (string, error) {
	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzWriter)

	for name, content := range entries {
		data := []byte(content)
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(data)),
		}
		if err := tarWriter.WriteHeader(hdr); err != nil {
			return "", err
		}
		if _, err := tarWriter.Write(data); err != nil {
			return "", err
		}
	}

	if err := tarWriter.Close(); err != nil {
		return "", err
	}
	if err := gzWriter.Close(); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func TestUnpackDirectoryRejectsTraversal(t *testing.T) {
	encoded, err := buildTarGz(map[string]string{
		"../escape.txt": "nope",
	})
	if err != nil {
		t.Fatalf("build archive: %v", err)
	}

	dir := t.TempDir()
	if err := UnpackDirectory(encoded, dir); err == nil {
		t.Fatalf("expected traversal error, got nil")
	}
}

func TestUnpackDirectoryWritesFiles(t *testing.T) {
	encoded, err := buildTarGz(map[string]string{
		"ok.txt": "hello",
	})
	if err != nil {
		t.Fatalf("build archive: %v", err)
	}

	dir := t.TempDir()
	if err := UnpackDirectory(encoded, dir); err != nil {
		t.Fatalf("unpack: %v", err)
	}

	path := filepath.Join(dir, "ok.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q, want %q", string(data), "hello")
	}
}
