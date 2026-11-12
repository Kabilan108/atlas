package jira

import (
	"context"
	"net/http"
	"testing"
)

func TestListFieldsAndResolveIDs(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/field" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(`[{"id":"summary","key":"summary","name":"Summary","custom":false},
			{"id":"customfield_10153","key":"customfield_10153","name":"Product","custom":true,"schema":{"type":"array","items":"option"}}]`))
	})
	fields, err := c.ListFields(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids, err := ResolveFieldIDs(fields, []string{"product", "summary"})
	if err != nil || len(ids) != 2 || ids[0] != "customfield_10153" || ids[1] != "summary" {
		t.Fatalf("ResolveFieldIDs = %v, %v", ids, err)
	}
	if _, err := ResolveFieldIDs(fields, []string{"Nope"}); err == nil {
		t.Error("unknown field resolved")
	}
}

func TestRenderedMedia(t *testing.T) {
	html := `<p><span class="image-wrap"><img src="https://x.atlassian.net/rest/api/3/attachment/thumbnail/11085" data-attachment-name="image001[11].png" data-attachment-type="thumbnail" data-media-services-id="d8b2" data-media-services-type="file" /></span>
	<a href="https://x.atlassian.net/rest/api/3/attachment/content/11090" data-attachment-name="log &amp; trace.txt" data-media-services-id="e9c3">log</a>
	<img src="https://x.atlassian.net/images/icons/emoji.png" data-media-services-id="nolink" /></p>`
	got := RenderedMedia(html)
	if len(got) != 2 {
		t.Fatalf("RenderedMedia = %v", got)
	}
	if a := got["d8b2"]; a.AttachmentID != "11085" || a.Name != "image001[11].png" {
		t.Errorf("image = %+v", a)
	}
	if a := got["e9c3"]; a.AttachmentID != "11090" || a.Name != "log & trace.txt" {
		t.Errorf("link = %+v", a)
	}
}

func TestSiteURLs(t *testing.T) {
	site := "https://x.atlassian.net/"
	if got := BrowseURL(site, "CSM-1"); got != "https://x.atlassian.net/browse/CSM-1" {
		t.Error(got)
	}
	if got := CommentURL(site, "CSM-1", "15191"); got != "https://x.atlassian.net/browse/CSM-1?focusedCommentId=15191" {
		t.Error(got)
	}
	if got := AttachmentURL(site, "11085"); got != "https://x.atlassian.net/rest/api/3/attachment/content/11085" {
		t.Error(got)
	}
}

func TestAPIPathAllowed(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/rest/api/3/issue/CSM-1", true},
		{"/rest/api/3/search/jql?jql=project%3DCSM", true},
		{"/rest/api/2/issue/CSM-1/comment", true},
		{"/rest/agile/1.0/board", true},
		{"/rest/servicedeskapi/servicedesk", true},
		{"/rest/api/3/", false},
		{"/rest/api/3", false},
		{"rest/api/3/issue/CSM-1", false},
		{"https://evil.example/rest/api/3/issue", false},
		{"//evil.example/rest/api/3/issue", false},
		{"/rest/api/3/../../admin", false},
		{"/rest/api/3/%2e%2e/%2e%2e/admin", false},
		{"/rest/api/3/issue%2F..%2Fadmin", false},
		{`/rest/api/3/issue\..\admin`, false},
		{"/rest/api/3/issue\n", false},
		{"/rest/api/3/issue#frag", false},
		{"/rest/auth/1/session", false},
		{"/secure/admin", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := APIPathAllowed(tc.path); got != tc.want {
			t.Errorf("APIPathAllowed(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
