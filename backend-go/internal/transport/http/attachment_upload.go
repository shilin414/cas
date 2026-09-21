package http

import (
	"errors"
	"io"
	"net/http"

	"github.com/shilin414/cas/backend-go/internal/catalog"
)

const (
	attachmentFileMaxBytes      = 40 << 20
	attachmentImageMaxBytes     = 5 << 20
	attachmentMultipartMaxBytes = attachmentFileMaxBytes + (64 << 10)
	attachmentMultipartMemory   = 1 << 20
)

// readAttachmentUpload bounds the entire request before multipart parsing.
// ParseMultipartForm's maxMemory is only a spill threshold, not a size limit:
// without MaxBytesReader, concurrent oversized uploads can exhaust temp disk.
// Both upload routes use this boundary before storage or provider side effects.
func readAttachmentUpload(w http.ResponseWriter, r *http.Request) (*catalog.AttachmentInput, bool) {
	if r.ContentLength > attachmentMultipartMaxBytes {
		writeBare(w, http.StatusRequestEntityTooLarge, "attachment exceeds 40MB")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, attachmentMultipartMaxBytes)
	// This also covers direct handler calls; do not depend on net/http's cleanup.
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	if err := r.ParseMultipartForm(attachmentMultipartMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeBare(w, http.StatusRequestEntityTooLarge, "attachment exceeds 40MB")
		} else {
			writeBare(w, http.StatusBadRequest, "file or doc_url is required")
		}
		return nil, false
	}
	in := &catalog.AttachmentInput{AttachmentType: r.FormValue("type"), DocURL: r.FormValue("doc_url")}
	if in.AttachmentType == "" {
		in.AttachmentType = "file"
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		if !errors.Is(err, http.ErrMissingFile) || in.DocURL == "" {
			writeBare(w, http.StatusBadRequest, "file or doc_url is required")
			return nil, false
		}
		return in, true
	}
	defer file.Close()
	limit := int64(attachmentFileMaxBytes)
	if in.AttachmentType == "image" {
		limit = attachmentImageMaxBytes
	}
	if header.Size > limit {
		writeBare(w, http.StatusRequestEntityTooLarge, "attachment exceeds size limit")
		return nil, false
	}
	in.Filename = header.Filename
	in.Data, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		writeSimpleError(w, http.StatusBadGateway, "attachment read failed")
		return nil, false
	}
	if int64(len(in.Data)) > limit {
		writeBare(w, http.StatusRequestEntityTooLarge, "attachment exceeds size limit")
		return nil, false
	}
	return in, true
}
