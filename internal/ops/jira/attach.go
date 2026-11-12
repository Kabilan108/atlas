package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

// AttachFile is one file to upload.
type AttachFile struct {
	Filename string `json:"filename"`
	Content  string `json:"content" jsonschema:"file bytes, base64"`
}

// AttachInput is the input to jira_attach.
type AttachInput struct {
	IssueRef
	Files []AttachFile `json:"files" jsonschema:"files to upload: up to 20, 10 MB in all"`
}

// AttachedFile is one uploaded attachment.
type AttachedFile struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}

// Attached is the receipt for an upload.
type Attached struct {
	Key         string         `json:"key"`
	Attachments []AttachedFile `json:"attachments"`
}

var Attach = &ops.Op[AttachInput, *Attached]{
	Meta: ops.Meta{
		Name:        "jira_attach",
		Description: "Attach files to a Jira issue. Send each file's bytes base64-encoded with its file name.",
		Surfaces:    ops.Both,
	},
	Run: attach,
}

func attach(ctx context.Context, env ops.Env, in AttachInput) (*Attached, error) {
	key, err := in.Key()
	if err != nil {
		return nil, err
	}
	if len(in.Files) == 0 || len(in.Files) > MaxAttachFiles {
		return nil, ops.Errorf(ops.Invalid, "files must hold 1 to %d files", MaxAttachFiles)
	}
	total := 0
	files := make([]jiraapi.AttachmentFile, len(in.Files))
	for i, f := range in.Files {
		if err := validFilename(f.Filename); err != nil {
			return nil, ops.Errorf(ops.Invalid, "files[%d]: %w", i, err)
		}
		if base64.StdEncoding.DecodedLen(len(f.Content)) > MaxAttachBytes+2 {
			return nil, ops.Errorf(ops.Invalid, "files[%d] %s is over the %d-byte limit per call", i, f.Filename, MaxAttachBytes)
		}
		content, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(f.Content), ""))
		if err != nil {
			return nil, ops.Errorf(ops.Invalid, "files[%d] %s: content is not base64: %w", i, f.Filename, err)
		}
		total += len(content)
		files[i] = jiraapi.AttachmentFile{Name: f.Filename, Content: bytes.NewReader(content)}
	}
	if total > MaxAttachBytes {
		return nil, ops.Errorf(ops.Invalid, "files total %d bytes, over the %d-byte limit per call", total, MaxAttachBytes)
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return nil, err
	}
	site, err := siteURL(ctx, client)
	if err != nil {
		return nil, err
	}
	uploaded, err := client.UploadAttachments(ctx, key, files)
	if err != nil {
		return nil, err
	}
	receipt := &Attached{Key: key}
	for _, a := range uploaded {
		receipt.Attachments = append(receipt.Attachments, AttachedFile{ID: a.ID, Filename: a.Filename, Size: a.Size, URL: jiraapi.AttachmentURL(site, a.ID)})
	}
	return receipt, nil
}

func validFilename(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("filename is empty")
	case len(name) > 255:
		return fmt.Errorf("filename is longer than 255 bytes")
	case strings.ContainsAny(name, `/\`), name == ".", name == "..":
		return fmt.Errorf("filename %q must be a bare file name", name)
	case strings.ContainsFunc(name, unicode.IsControl):
		return fmt.Errorf("filename %q contains control characters", name)
	}
	return nil
}
