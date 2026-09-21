package aimodel

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

func invalid(field string) error { return fmt.Errorf("%w: %s", ErrInvalid, field) }
func validID(id string) bool {
	u, e := uuid.Parse(id)
	return e == nil && u != uuid.Nil && u.String() == id
}
func validText(s string, max int) bool {
	return utf8.ValidString(s) && len(s) <= max && !strings.ContainsRune(s, 0)
}
func (s *Service) validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery {
		return invalid("base_url")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.ContainsAny(host, "%\\ \t\r\n") || strings.HasSuffix(host, ".") {
		return invalid("base_url host")
	}
	if port := u.Port(); port != "" {
		var p int
		if _, e := fmt.Sscanf(port, "%d", &p); e != nil || p < 1 || p > 65535 {
			return invalid("base_url port")
		}
	}
	for _, allowed := range s.opts.AllowedPrivateHosts {
		if host == strings.ToLower(strings.TrimSpace(allowed)) {
			return nil
		}
	}
	if u.Scheme != "https" {
		return invalid("HTTP requires deployment allowlist")
	}
	ip := net.ParseIP(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") || (ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || !ip.IsGlobalUnicast())) {
		return invalid("private host requires deployment allowlist")
	}
	return nil
}
func (s *Service) validateConnection(in ConnectionInput) error {
	if strings.TrimSpace(in.Name) == "" || !validText(in.Name, 128) {
		return invalid("name")
	}
	if in.Adapter != "openai_chat" && in.Adapter != "gemini" {
		return invalid("adapter")
	}
	if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 600 {
		return invalid("timeout_seconds")
	}
	if in.MaxConcurrency < 1 || in.MaxConcurrency > 64 {
		return invalid("max_concurrency")
	}
	if in.Credential != nil {
		if err := validateCredential(*in.Credential); err != nil {
			return err
		}
	}
	return s.validateURL(in.BaseURL)
}
func validateCredential(v string) error {
	if !validText(v, 8192) || strings.ContainsAny(v, "\r\n") {
		return invalid("credential")
	}
	return nil
}
func validateParameters(p Parameters) error {
	if p.Temperature != nil && (math.IsNaN(*p.Temperature) || math.IsInf(*p.Temperature, 0) || *p.Temperature < 0 || *p.Temperature > 2) {
		return invalid("temperature")
	}
	if p.MaxOutputTokens != nil && (*p.MaxOutputTokens < 1 || *p.MaxOutputTokens > 32768) {
		return invalid("max_output_tokens")
	}
	return nil
}

type manifest struct {
	Adapter   string `json:"adapter"`
	Version   string `json:"version"`
	Resources []struct {
		Name      string `json:"name"`
		URL       string `json:"url"`
		SHA256    string `json:"sha256"`
		SizeBytes int64  `json:"size_bytes"`
	} `json:"resources"`
}

func decodeStrict(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return ErrInvalid
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return ErrInvalid
	}
	return nil
}
func (s *Service) validateModel(in *ModelInput) error {
	if strings.TrimSpace(in.Name) == "" || !validText(in.Name, 128) {
		return invalid("name")
	}
	if in.ExecutionLocation == "browser_local" && in.ModelID == "" {
		in.ModelID = "PP-OCRv6_tiny"
	}
	if strings.TrimSpace(in.ModelID) == "" || !validText(in.ModelID, 255) {
		return invalid("model_id")
	}
	if err := validateParameters(in.DefaultParameters); err != nil {
		return err
	}
	switch in.ExecutionLocation {
	case "server_remote":
		if in.CapabilityKind != "chat" || in.ConnectionID == nil || !validID(*in.ConnectionID) {
			return invalid("remote model connection/capability")
		}
		if len(in.BrowserManifest) > 0 && string(in.BrowserManifest) != "null" {
			return invalid("remote browser_manifest")
		}
	case "browser_local":
		if in.CapabilityKind != "ocr" || in.ConnectionID != nil || in.ModelID != "PP-OCRv6_tiny" {
			return invalid("local model configuration")
		}
		if in.Capabilities.Video || in.Capabilities.PDF || in.Capabilities.Streaming {
			return invalid("local OCR capabilities")
		}
		if len(in.BrowserManifest) > MaxManifestBytes {
			return invalid("browser_manifest size")
		}
		var m manifest
		if err := decodeStrict(in.BrowserManifest, &m); err != nil {
			return invalid("browser_manifest")
		}
		if m.Adapter != "paddleocr_tiny" || len(m.Version) > 128 || !safePathSegment(m.Version) || len(m.Resources) != 2 {
			return invalid("browser_manifest adapter/version/resources")
		}
		var size int64
		seen := map[string]bool{}
		for _, r := range m.Resources {
			if (r.Name != "PP-OCRv6_tiny_det" && r.Name != "PP-OCRv6_tiny_rec") || seen[r.Name] {
				return invalid("resource name")
			}
			seen[r.Name] = true
			hash, e := hex.DecodeString(r.SHA256)
			if e != nil || len(hash) != 32 || r.SizeBytes <= 0 || r.SizeBytes > 64<<20 {
				return invalid("resource integrity")
			}
			size += r.SizeBytes
			u, e := url.Parse(r.URL)
			if e != nil || len(r.URL) > 2048 || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || strings.ContainsAny(r.URL, "%\\") || strings.HasPrefix(r.URL, "//") || path.Clean(u.Path) != u.Path {
				return invalid("resource URL")
			}
			portable := "ocr-assets/" + m.Version + "/" + r.Name + ".tar"
			if r.URL == portable {
				continue
			}
			if !strings.HasPrefix(r.URL, "/") {
				return invalid("resource URL")
			}
			suffix := "/" + portable
			if !strings.HasSuffix(u.Path, suffix) {
				return invalid("resource URL")
			}
			prefix := strings.TrimSuffix(u.Path, suffix)
			for _, segment := range strings.Split(strings.TrimPrefix(prefix, "/"), "/") {
				if segment != "" && !safePathSegment(segment) {
					return invalid("resource base path")
				}
			}
		}
		if size > 128<<20 {
			return invalid("resource total size")
		}

	default:
		return invalid("execution_location")
	}
	return nil
}
func validateMessage(in MessageInput) error {
	if !validID(in.RequestID) || !validText(in.Text, MaxOutputBytes) || !validText(in.SystemPrompt, 65536) || len(in.AttachmentIDs) > 4 {
		return invalid("message")
	}
	if strings.TrimSpace(in.Text) == "" && len(in.AttachmentIDs) == 0 {
		return invalid("empty message")
	}
	seen := map[string]bool{}
	for _, id := range in.AttachmentIDs {
		if !validID(id) || seen[id] {
			return invalid("attachment_ids")
		}
		seen[id] = true
	}
	return validateParameters(in.Parameters)
}
func validateAttachment(in AttachmentInput) error {
	if strings.TrimSpace(in.Name) == "" || !validText(in.Name, 255) || in.StorageKey == "" || !validText(in.StorageKey, 512) || in.SizeBytes <= 0 || in.SizeBytes > MaxAttachmentBytes {
		return invalid("attachment")
	}
	switch in.Kind {
	case "image":
		if in.MIMEType != "image/png" && in.MIMEType != "image/jpeg" && in.MIMEType != "image/webp" && in.MIMEType != "image/gif" {
			return invalid("image MIME")
		}
	case "video":
		if in.MIMEType != "video/mp4" && in.MIMEType != "video/webm" && in.MIMEType != "video/quicktime" {
			return invalid("video MIME")
		}
	case "pdf":
		if in.MIMEType != "application/pdf" {
			return invalid("PDF MIME")
		}
	default:
		return invalid("attachment kind")
	}
	return nil
}

func safePathSegment(v string) bool {
	if v == "" || v == "." || v == ".." {
		return false
	}
	for _, c := range v {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}
