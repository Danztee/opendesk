// Package mailsource is how a message's raw source (its headers and body as the mail
// server delivered them) is shown and downloaded: Show original in tickets and in Collab.
package mailsource

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// ViewBytes is how much of a source the app shows; the download is the whole message.
	ViewBytes = 1 << 20
	// MaxBytes is the largest source read at all (a mail with its attachments).
	MaxBytes = 40 << 20
)

// MailSource is a message's source as the app shows it.
type MailSource struct {
	Source    string `json:"source" doc:"The raw message: its headers, a blank line and its body, as text. Invalid UTF-8 is replaced."`
	SizeBytes int    `json:"size_bytes" doc:"The whole message's size"`
	Truncated bool   `json:"truncated" doc:"Only the first megabyte is shown; the download has all of it"`
}

// View is raw as text for the app: the first ViewBytes of it, cut at a line, with
// anything that isn't valid UTF-8 replaced.
func View(raw []byte) MailSource {
	out := MailSource{SizeBytes: len(raw)}
	if len(raw) > ViewBytes {
		raw, out.Truncated = raw[:ViewBytes], true
		if i := strings.LastIndexByte(string(raw), '\n'); i > 0 {
			raw = raw[:i+1]
		}
	}
	out.Source = strings.ToValidUTF8(string(raw), "�")
	return out
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Filename names a message's download after its subject: letters, digits and . _ - only,
// 60 characters at most, ending in .eml.
func Filename(subject string) string {
	name := strings.Trim(unsafeName.ReplaceAllString(strings.TrimSpace(subject), "-"), "-._")
	if utf8.RuneCountInString(name) > 60 {
		name = string([]rune(name)[:60])
	}
	if name == "" {
		name = "message"
	}
	return name + ".eml"
}

// File is a source's download: the raw message with headers that make a browser save it,
// never render it, and keep anything inside it from running.
type File struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	NoSniff            string `header:"X-Content-Type-Options"`
	CSP                string `header:"Content-Security-Policy"`
	Body               []byte
}

// Download is the response for raw, named after subject.
func Download(raw []byte, subject string) *File {
	return &File{ContentType: "message/rfc822", ContentDisposition: `attachment; filename="` + Filename(subject) + `"`,
		NoSniff: "nosniff", CSP: "default-src 'none'; sandbox", Body: raw}
}
