package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strings"

	"github.com/shilin414/cas/backend-go/internal/platform/storage"
)

const cardAvatarMaxBytes = 2 * 1024 * 1024

// UploadStoredCardAvatar reads only a pinned application avatar object. It never
// fetches arbitrary URLs or changes sender identity to work around permissions.
func (c *FeishuClient) UploadStoredCardAvatar(ctx context.Context, token string, store storage.Storage, key string) (string, error) {
	if store == nil || !strings.HasPrefix(key, "application-avatars/") {
		return "", fmt.Errorf("avatar unavailable")
	}
	clean, err := storage.SanitizeKey(key)
	if err != nil || clean != key {
		return "", fmt.Errorf("invalid avatar reference")
	}
	reader, obj, err := store.Open(ctx, key)
	if err != nil {
		return "", fmt.Errorf("avatar unavailable: %w", err)
	}
	defer reader.Close()
	if obj.Size <= 0 || obj.Size > cardAvatarMaxBytes {
		return "", fmt.Errorf("avatar size invalid")
	}
	data, err := io.ReadAll(io.LimitReader(reader, cardAvatarMaxBytes+1))
	if err != nil || len(data) > cardAvatarMaxBytes || int64(len(data)) != obj.Size {
		return "", fmt.Errorf("avatar read failed")
	}
	switch http.DetectContentType(data) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return "", fmt.Errorf("avatar image type invalid")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("image_type", "message"); err != nil {
		return "", err
	}
	file, err := form.CreateFormFile("image", path.Base(key))
	if err != nil {
		return "", err
	}
	if _, err = file.Write(data); err != nil {
		return "", err
	}
	if err = form.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/open-apis/im/v1/images", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload card avatar: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ImageKey string `json:"image_key"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", fmt.Errorf("invalid avatar upload response")
	}
	if result.Code == nil {
		return "", fmt.Errorf("avatar upload success code missing")
	}
	if *result.Code != 0 {
		return "", &FeishuAPIError{Path: "/open-apis/im/v1/images", Code: *result.Code, Msg: result.Msg}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || result.Data.ImageKey == "" {
		return "", fmt.Errorf("avatar upload was not confirmed")
	}
	return result.Data.ImageKey, nil
}
