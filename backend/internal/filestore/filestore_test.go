package filestore

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestPublicFilesUseSeparateNamespace 确认普通 key 加 public 前缀不能定位私有目录中的文件。
func TestPublicFilesUseSeparateNamespace(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := manager.Store(context.Background(), "local", "private.png", strings.NewReader("private"), 64)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := manager.StorePublic(context.Background(), "local", "feedback.png", strings.NewReader("public"), 64)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseKey(publicKey)
	if err != nil || !parsed.Public {
		t.Fatalf("public storekey was not marked: key=%q parsed=%+v error=%v", publicKey, parsed, err)
	}
	publicFile, _, _, err := manager.Open(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = publicFile.Close()

	forgedPublicKey := "public_" + privateKey
	if _, _, _, err := manager.Open(forgedPublicKey); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("adding the public marker to a private key opened the file: error=%v", err)
	}
	strippedPrivateKey := strings.TrimPrefix(publicKey, "public_")
	if _, _, _, err := manager.Open(strippedPrivateKey); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("removing the public marker opened a private file: error=%v", err)
	}

	reader, _, _, err := manager.Open(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(content) != "private" {
		t.Fatalf("private file read changed: content=%q error=%v", content, err)
	}
}

// TestParseKeyKeepsLegacyPrivateFormat 确认新增公共标记不改变历史私有 storekey 的解析。
func TestParseKeyKeepsLegacyPrivateFormat(t *testing.T) {
	key, err := ParseKey("local_20261016_123e4567-e89b-12d3-a456-426614174000.png")
	if err != nil {
		t.Fatal(err)
	}
	if key.Public || key.Type != "local" || key.Extension != "png" {
		t.Fatalf("legacy private key parsed unexpectedly: %+v", key)
	}
}
