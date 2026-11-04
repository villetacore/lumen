package integrations

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Feed reads RSS and Atom: blogs, release notes, status pages, GitHub and GitLab
// activity feeds, anything that publishes one.
type Feed struct{}

func (*Feed) Kind() string { return "feed" }

func (*Feed) Title() string { return "RSS и Atom" }

func (*Feed) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "url", Label: "Адрес ленты", Type: FieldURL, Required: true,
			Placeholder: "https://example.com/feed.xml"},
		{Key: "as_tasks", Label: "Создавать задачи, а не заметки", Type: FieldBool,
			Help: "Удобно, если каждую запись нужно разобрать"},
		{Key: "allow_private", Label: "Разрешить приватные адреса", Type: FieldBool,
			Help: "Нужно для ленты во внутренней сети"},
	}
}

func (*Feed) Validate(c Config) error {
	u, err := url.Parse(c.Value("url", ""))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("адрес указывается как https://example.com/feed.xml")
	}
	return nil
}

// feedDoc matches both formats: RSS keeps items under channel, Atom has entries.
type feedDoc struct {
	Items   []feedEntry `xml:"channel>item"`
	Entries []feedEntry `xml:"entry"`
}

type feedEntry struct {
	GUID  string `xml:"guid"`
	ID    string `xml:"id"`
	Title string `xml:"title"`
	Links []struct {
		Href string `xml:"href,attr"` // Atom
		Rel  string `xml:"rel,attr"`
		Text string `xml:",chardata"` // RSS
	} `xml:"link"`
	Description string `xml:"description"`
	Summary     string `xml:"summary"`
	Content     string `xml:"content"`
	Published   string `xml:"pubDate"`
	Updated     string `xml:"updated"`
}

func (e feedEntry) link() string {
	for _, l := range e.Links {
		if l.Href != "" && (l.Rel == "" || l.Rel == "alternate") {
			return l.Href
		}
	}
	for _, l := range e.Links {
		if t := strings.TrimSpace(l.Text); t != "" {
			return t
		}
	}
	return ""
}

func (f *Feed) Sync(ctx context.Context, in SyncInput) (SyncOutput, error) {
	out := SyncOutput{State: map[string]string{}}
	body, _, err := Fetch(ctx, in.HTTP, in.Config.Value("url", ""), map[string]string{"Accept": "application/rss+xml, application/atom+xml, application/xml"})
	if err != nil {
		return out, err
	}
	var doc feedDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		return out, fmt.Errorf("это не RSS и не Atom: %w", err)
	}
	kind := ItemNote
	if in.Config.Bool("as_tasks") {
		kind = ItemTask
	}
	for _, e := range append(doc.Items, doc.Entries...) {
		link := e.link()
		id := strings.TrimSpace(e.GUID)
		if id == "" {
			id = strings.TrimSpace(e.ID)
		}
		if id == "" {
			id = link
		}
		title := strings.TrimSpace(e.Title)
		if id == "" || title == "" {
			continue
		}
		text := e.Content
		if text == "" {
			text = e.Summary
		}
		if text == "" {
			text = e.Description
		}
		item := ExternalItem{ExternalID: id, Kind: kind, Title: title, Body: strings.TrimSpace(text), URL: link}
		if t := feedTime(e); !t.IsZero() {
			item.Start = t
		}
		item.Hash = hashItem(item)
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func feedTime(e feedEntry) time.Time {
	for _, raw := range []string{e.Published, e.Updated} {
		for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC3339} {
			if t, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}
