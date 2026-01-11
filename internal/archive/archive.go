package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PackDirectory packs a directory into a base64-encoded tar.gz string
// This is suitable for storing in a Gist file
func PackDirectory(dirPath string) (string, error) {
	// Check if directory exists
	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty archive for non-existent directory
			return "", nil
		}
		return "", fmt.Errorf("failed to stat directory: %w", err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", dirPath)
	}

	var buf bytes.Buffer

	// Create gzip writer
	gzWriter := gzip.NewWriter(&buf)

	// Create tar writer
	tarWriter := tar.NewWriter(gzWriter)

	// Walk the directory
	err = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		// Skip hidden files and directories (except the content)
		baseName := filepath.Base(path)
		if strings.HasPrefix(baseName, ".") && baseName != "." {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header: %w", err)
		}

		// Use relative path in archive
		header.Name = relPath

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		// If it's a file, write its content
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file: %w", err)
			}
			defer file.Close()

			if _, err := io.Copy(tarWriter, file); err != nil {
				return fmt.Errorf("failed to write file content: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to walk directory: %w", err)
	}

	// Close writers
	if err := tarWriter.Close(); err != nil {
		return "", fmt.Errorf("failed to close tar writer: %w", err)
	}

	if err := gzWriter.Close(); err != nil {
		return "", fmt.Errorf("failed to close gzip writer: %w", err)
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	return encoded, nil
}

// UnpackDirectory unpacks a base64-encoded tar.gz string to a directory
func UnpackDirectory(encoded string, dirPath string) error {
	if encoded == "" {
		// Empty archive, create empty directory
		return os.MkdirAll(dirPath, 0755)
	}

	// Decode base64
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("failed to decode base64: %w", err)
	}

	// Create gzip reader
	gzReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Create tar reader
	tarReader := tar.NewReader(gzReader)

	// Create target directory
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Extract files
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// Construct target path
		targetPath := filepath.Join(dirPath, header.Name)

		// Security check: ensure path is within target directory
		cleanDir := filepath.Clean(dirPath)
		cleanTarget := filepath.Clean(targetPath)
		rel, err := filepath.Rel(cleanDir, cleanTarget)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return fmt.Errorf("failed to write file content: %w", err)
			}
			file.Close()
		}
	}

	return nil
}

// GetDirectoryHash calculates a hash for a directory's contents
// Used for detecting changes
func GetDirectoryHash(dirPath string) (string, error) {
	content, err := PackDirectory(dirPath)
	if err != nil {
		return "", err
	}

	// Use first 32 chars of base64 as a quick hash
	// This is not cryptographically secure but sufficient for change detection
	if len(content) > 32 {
		return content[:32], nil
	}
	return content, nil
}

// ListDirectoryFiles returns a list of files in a directory
func ListDirectoryFiles(dirPath string) ([]string, error) {
	var files []string

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			relPath, err := filepath.Rel(dirPath, path)
			if err != nil {
				return err
			}
			files = append(files, relPath)
		}

		return nil
	})

	return files, err
}

// DirectoryExists checks if a directory exists and is not empty
func DirectoryExists(dirPath string) bool {
	info, err := os.Stat(dirPath)
	if err != nil {
		return false
	}

	if !info.IsDir() {
		return false
	}

	// Check if directory has any contents
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return false
	}

	return len(entries) > 0
}
