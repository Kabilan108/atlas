package jiratest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
)

type linkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

type issueKey struct {
	Key string `json:"key"`
}

type storedLink struct {
	id              string
	typ             linkType
	inward, outward string
}

// Links serves issue links the way Jira stores them. typesJSON is the
// {"issueLinkTypes": [...]} answer to GET /rest/api/3/issueLinkType.
// POST /rest/api/3/issueLink records a link, and GET /rest/api/3/issue/{key}
// for each of keys lists that issue's links, derived from what was posted.
//
// Atlassian's model: a posted link reads "inwardIssue <outward> outwardIssue"
// ({"inwardIssue": A, "outwardIssue": B} with type Blocks is "A blocks B").
// A then shows the link with outwardIssue B, and B shows it with
// inwardIssue A ("B is blocked by A").
func (f *Fake) Links(typesJSON string, keys ...string) {
	var types struct {
		IssueLinkTypes []linkType `json:"issueLinkTypes"`
	}
	if err := json.Unmarshal([]byte(typesJSON), &types); err != nil {
		panic(fmt.Sprintf("jiratest: link types: %v", err))
	}
	var mu sync.Mutex
	var links []storedLink
	f.JSON("GET /rest/api/3/issueLinkType", http.StatusOK, typesJSON)
	f.Handle("POST /rest/api/3/issueLink", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Type struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"type"`
			InwardIssue  issueKey `json:"inwardIssue"`
			OutwardIssue issueKey `json:"outwardIssue"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InwardIssue.Key == "" || req.OutwardIssue.Key == "" {
			Write(w, http.StatusBadRequest, `{"errorMessages":["jiratest: bad issueLink body"]}`)
			return
		}
		for _, t := range types.IssueLinkTypes {
			if t.Name == req.Type.Name || (req.Type.ID != "" && t.ID == req.Type.ID) {
				mu.Lock()
				links = append(links, storedLink{id: strconv.Itoa(len(links) + 1), typ: t, inward: req.InwardIssue.Key, outward: req.OutwardIssue.Key})
				mu.Unlock()
				Write(w, http.StatusCreated, ``)
				return
			}
		}
		Write(w, http.StatusNotFound, fmt.Sprintf(`{"errorMessages":["No issue link type with name '%s' found."]}`, req.Type.Name))
	})
	for _, key := range keys {
		f.Handle("GET /rest/api/3/issue/"+key, func(w http.ResponseWriter, _ *http.Request) {
			type shown struct {
				ID           string    `json:"id"`
				Type         linkType  `json:"type"`
				InwardIssue  *issueKey `json:"inwardIssue,omitempty"`
				OutwardIssue *issueKey `json:"outwardIssue,omitempty"`
			}
			list := []shown{}
			mu.Lock()
			for _, l := range links {
				switch key {
				case l.inward:
					list = append(list, shown{ID: l.id, Type: l.typ, OutwardIssue: &issueKey{l.outward}})
				case l.outward:
					list = append(list, shown{ID: l.id, Type: l.typ, InwardIssue: &issueKey{l.inward}})
				}
			}
			mu.Unlock()
			data, _ := json.Marshal(map[string]any{"key": key, "fields": map[string]any{"issuelinks": list}})
			Write(w, http.StatusOK, string(data))
		})
	}
}
