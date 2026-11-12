package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

// AttachFile is one file to upload: Text as is, or Content decoded from
// base64.
type AttachFile struct {
	Filename string  `json:"filename"`
	Text     *string `json:"text,omitempty" jsonschema:"UTF-8 content sent as is (logs, JSON, CSV); use instead of content"`
	Content  string  `json:"content,omitempty" jsonschema:"file bytes, base64, for binaries"`
}

// AttachInput is the input to jira_attach.
type AttachInput struct {
	IssueRef
	Files []AttachFile `json:"files" jsonschema:"up to 20 files, 10 MB in all after decoding"`
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
		Description: "Attach files to a Jira issue. Give each file a filename and either text or base64 content. The receipt has each file's URL to link from a description or comment.",
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
	sizes := make([]int, len(in.Files))
	total := 0
	for i, f := range in.Files {
		if err := validFilename(f.Filename); err != nil {
			return nil, ops.Errorf(ops.Invalid, "files[%d]: %w", i, err)
		}
		var size int
		switch {
		case f.Text != nil && f.Content != "":
			return nil, ops.Errorf(ops.Invalid, "files[%d] %s: give text or content, not both", i, f.Filename)
		case f.Text != nil:
			size = len(*f.Text)
		case f.Content == "":
			return nil, ops.Errorf(ops.Invalid, "files[%d] %s: give its text, or its bytes base64-encoded as content", i, f.Filename)
		default:
			if size, err = decodedSize(f.Content); err != nil {
				return nil, ops.Errorf(ops.Invalid, "files[%d] %s: content is not base64: %w", i, f.Filename, err)
			}
		}
		sizes[i] = size
		if total += size; total > MaxAttachBytes {
			return nil, ops.Errorf(ops.Invalid, "files[%d] %s brings the files to %d bytes, over the %d-byte limit per call", i, f.Filename, total, MaxAttachBytes)
		}
	}
	files := make([]jiraapi.AttachmentFile, len(in.Files))
	for i, f := range in.Files {
		if f.Text != nil {
			files[i] = jiraapi.AttachmentFile{Name: f.Filename, Content: strings.NewReader(*f.Text)}
			continue
		}
		content, err := decodeBase64(f.Content, sizes[i])
		if err != nil {
			return nil, ops.Errorf(ops.Invalid, "files[%d] %s: content is not base64: %w", i, f.Filename, err)
		}
		files[i] = jiraapi.AttachmentFile{Name: f.Filename, Content: bytes.NewReader(content)}
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

func isBase64Space(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

// decodedSize is the byte count padded standard base64 decodes to, from
// its length alone (whitespace ignored), so a size limit applies before
// anything is decoded.
func decodedSize(content string) (int, error) {
	n, pad := 0, 0
	for i := 0; i < len(content); i++ {
		switch b := content[i]; {
		case isBase64Space(b):
		case b == '=':
			n++
			pad++
		default:
			if pad > 0 {
				return 0, errors.New("data after padding")
			}
			n++
		}
	}
	if n%4 != 0 || pad > 2 {
		return 0, fmt.Errorf("length %d is not a multiple of 4", n)
	}
	return n/4*3 - pad, nil
}

// decodeBase64 decodes content into exactly size bytes, streaming past
// whitespace instead of copying it out first.
func decodeBase64(content string, size int) ([]byte, error) {
	out := make([]byte, size)
	dec := base64.NewDecoder(base64.StdEncoding, spaceSkipper{strings.NewReader(content)})
	if _, err := io.ReadFull(dec, out); err != nil {
		return nil, err
	}
	if n, err := dec.Read(make([]byte, 1)); n > 0 || (err != nil && err != io.EOF) {
		if err == nil {
			err = errors.New("more data than its length says")
		}
		return nil, err
	}
	return out, nil
}

// spaceSkipper drops ASCII whitespace, which base64.StdEncoding rejects
// apart from newlines.
type spaceSkipper struct{ r io.Reader }

func (s spaceSkipper) Read(p []byte) (int, error) {
	for {
		n, err := s.r.Read(p)
		kept := 0
		for _, b := range p[:n] {
			if !isBase64Space(b) {
				p[kept] = b
				kept++
			}
		}
		if kept > 0 || err != nil {
			return kept, err
		}
	}
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
