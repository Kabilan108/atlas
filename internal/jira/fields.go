package jira

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Field is one entry of the site's field list: system and custom fields,
// with the ID that requests and search results use.
type Field struct {
	ID     string       `json:"id"`
	Key    string       `json:"key,omitempty"`
	Name   string       `json:"name"`
	Custom bool         `json:"custom"`
	Schema *FieldSchema `json:"schema,omitempty"`
}

// ListFields returns every field on the site.
func (c *Client) ListFields(ctx context.Context) ([]Field, error) {
	var fields []Field
	if err := c.getJSON(ctx, "/rest/api/3/field", nil, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// ResolveFieldIDs maps field names or IDs to field IDs with ResolveField's
// rules (an ID, then an exact name, then a case-insensitive name).
func ResolveFieldIDs(fields []Field, names []string) ([]string, error) {
	metas := make([]FieldMeta, len(fields))
	for i, f := range fields {
		metas[i] = FieldMeta{FieldID: f.ID, Key: f.Key, Name: f.Name}
	}
	ids := make([]string, 0, len(names))
	for _, name := range names {
		meta, err := ResolveField(metas, name)
		if err != nil {
			return nil, err
		}
		ids = append(ids, meta.ID())
	}
	return ids, nil
}

// RenderedAttachment is the attachment Jira's rendered HTML shows for a
// media node.
type RenderedAttachment struct {
	AttachmentID string
	Name         string
}

var (
	mediaTag      = regexp.MustCompile(`<[a-zA-Z][^>]*\bdata-media-services-id="[^"]+"[^>]*>`)
	mediaIDAttr   = regexp.MustCompile(`\bdata-media-services-id="([^"]+)"`)
	nameAttr      = regexp.MustCompile(`\bdata-attachment-name="([^"]*)"`)
	attachmentURL = regexp.MustCompile(`/attachment/(?:content|thumbnail)/(\d+)`)
)

// RenderedMedia reads the media in renderedFields or renderedBody HTML and
// maps each Media Services ID (an ADF media node's id) to the attachment
// it shows. ADF media IDs are not attachment IDs, and media nodes rarely
// carry a file name, so this is the reliable link between the two. Media
// the HTML omits (Jira leaves out some inside expands) are not mapped.
func RenderedMedia(renderedHTML string) map[string]RenderedAttachment {
	found := map[string]RenderedAttachment{}
	for _, tag := range mediaTag.FindAllString(renderedHTML, -1) {
		id := mediaIDAttr.FindStringSubmatch(tag)
		ref := attachmentURL.FindStringSubmatch(tag)
		if id == nil || ref == nil {
			continue
		}
		att := RenderedAttachment{AttachmentID: ref[1]}
		if name := nameAttr.FindStringSubmatch(tag); name != nil {
			att.Name = html.UnescapeString(name[1])
		}
		found[html.UnescapeString(id[1])] = att
	}
	return found
}

// AttachmentURL is where a signed-in browser opens an attachment on the
// site at siteURL (ServerInfo's BaseURL).
func AttachmentURL(siteURL, attachmentID string) string {
	return fmt.Sprintf("%s/rest/api/3/attachment/content/%s", strings.TrimRight(siteURL, "/"), escape(attachmentID))
}

// BrowseURL is an issue's page on the site at siteURL.
func BrowseURL(siteURL, issueKey string) string {
	return strings.TrimRight(siteURL, "/") + "/browse/" + escape(issueKey)
}

// CommentURL opens an issue's page at one comment.
func CommentURL(siteURL, issueKey, commentID string) string {
	return BrowseURL(siteURL, issueKey) + "?focusedCommentId=" + escape(commentID)
}
