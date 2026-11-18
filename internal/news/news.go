// Package news reads YARG news from the same source the official YARC launcher shows.
//
//	index:    <Server>/index.json            -> {articles: [{md, title, type, release, authors, category, ...}]}
//	article:  <Server>/articles/<md>.md      -> Markdown with a YAML front matter block
//	author:   <Server>/authors/<id>.json     -> {displayName, role?, avatar?}
//
// Articles are turned into plain text so a screen reader can read them line by line.
package news

import (
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/netx"
)

const Server = "https://raw.githubusercontent.com/YARC-Official/News/master"

var typeLabels = map[string]string{"update": "Update", "announcement": "Announcement"}

type Article struct {
	MD       string
	Title    string
	Type     string
	Release  string
	Authors  []string
	Category string
}

func (a *Article) Date() string {
	if len(a.Release) > 10 {
		return a.Release[:10]
	}
	return a.Release
}

func (a *Article) TypeLabel() string {
	if l, ok := typeLabels[a.Type]; ok {
		return l
	}
	s := strings.ReplaceAll(a.Type, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func FetchIndex() ([]*Article, error) {
	var data struct {
		Articles []struct {
			MD       string   `json:"md"`
			Title    string   `json:"title"`
			Type     string   `json:"type"`
			Release  string   `json:"release"`
			Authors  []string `json:"authors"`
			Category string   `json:"category"`
		} `json:"articles"`
	}
	if err := netx.GetJSON(Server+"/index.json", nil, &data); err != nil {
		return nil, err
	}
	var out []*Article
	for _, a := range data.Articles {
		title := a.Title
		if title == "" {
			title = a.MD
		}
		out = append(out, &Article{a.MD, title, a.Type, a.Release, a.Authors, a.Category})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Release > out[j].Release })
	return out, nil
}

var (
	authorMu    sync.Mutex
	authorCache = map[string]string{}
)

func AuthorName(id string) string {
	authorMu.Lock()
	name, ok := authorCache[id]
	authorMu.Unlock()
	if ok {
		return name
	}
	var data struct {
		DisplayName string `json:"displayName"`
	}
	name = id
	if netx.GetJSON(Server+"/authors/"+id+".json", nil, &data) == nil && data.DisplayName != "" {
		name = data.DisplayName
	}
	authorMu.Lock()
	authorCache[id] = name
	authorMu.Unlock()
	return name
}

func FetchArticleMarkdown(md string) (string, error) {
	b, err := netx.GetBytes(Server+"/articles/"+md+".md", nil)
	if err != nil {
		return "", err
	}
	return strings.ToValidUTF8(string(b), "�"), nil
}

func StripFrontMatter(text string) string {
	if strings.HasPrefix(text, "---") {
		if end := strings.Index(text[3:], "\n---"); end != -1 {
			return strings.TrimLeft(text[3+end+4:], "\n")
		}
	}
	return text
}

var (
	comments  = regexp.MustCompile(`(?s)<!--.*?-->`)
	images    = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	links     = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)[^)]*\)`)
	htmlTags  = regexp.MustCompile(`<[^>]+>`)
	heading   = regexp.MustCompile(`^\s*#{1,6}\s+(.*)`)
	bullet    = regexp.MustCompile(`^(\s*)[*+]\s+`)
	strong    = regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)
	rule      = regexp.MustCompile(`^\s*(-{3,}|\*{3,}|_{3,})\s*$`)
	manyBlank = regexp.MustCompile(`\n{3,}`)
	nonAlnum  = regexp.MustCompile(`[^a-z0-9]`)
)

func isWord(r byte) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= 0x80
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

// dropEmphasis removes single * or _ emphasis markers, like Python's
// (?<![\w*])[*_](?!\s)(.+?)(?<!\s)[*_](?![\w*]). Go's regexp has no lookarounds, so it scans.
func dropEmphasis(line string) string {
	var out strings.Builder
	i := 0
	for i < len(line) {
		c := line[i]
		if (c == '*' || c == '_') && (i == 0 || !(isWord(line[i-1]) || line[i-1] == '*')) &&
			i+1 < len(line) && !isSpace(line[i+1]) {
			// The shortest match: the first closing marker after at least one character.
			for j := i + 2; j < len(line); j++ {
				if (line[j] == '*' || line[j] == '_') && !isSpace(line[j-1]) &&
					(j+1 == len(line) || !(isWord(line[j+1]) || line[j+1] == '*')) {
					out.WriteString(line[i+1 : j])
					i = j + 1
					goto next
				}
			}
		}
		out.WriteByte(c)
		i++
	next:
	}
	return out.String()
}

// MarkdownToText makes readable plain text: headings on their own lines, link text followed by the
// address, images and HTML removed, emphasis markers dropped, list bullets kept.
func MarkdownToText(md string) string {
	text := strings.ReplaceAll(StripFrontMatter(md), "\r\n", "\n")
	text = comments.ReplaceAllString(text, "")
	text = images.ReplaceAllStringFunc(text, func(m string) string {
		alt := images.FindStringSubmatch(m)[1]
		if alt == "" {
			return ""
		}
		return "(Image: " + alt + ")"
	})
	text = links.ReplaceAllStringFunc(text, func(m string) string {
		g := links.FindStringSubmatch(m)
		if g[1] == g[2] {
			return g[1]
		}
		return g[1] + " (" + g[2] + ")"
	})
	text = htmlTags.ReplaceAllString(text, "")
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if h := heading.FindStringSubmatch(line); h != nil {
			line = strings.TrimSpace(h[1])
		}
		line = bullet.ReplaceAllString(line, "$1- ")
		line = strong.ReplaceAllStringFunc(line, func(m string) string {
			g := strong.FindStringSubmatch(m)
			return g[1] + g[2]
		})
		line = dropEmphasis(line)
		line = strings.ReplaceAll(line, "`", "")
		if rule.MatchString(line) {
			line = ""
		}
		lines = append(lines, strings.TrimRight(line, " \t\r\f\v"))
	}
	text = strings.Join(lines, "\n")
	return strings.TrimSpace(manyBlank.ReplaceAllString(text, "\n\n"))
}

func letters(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

// ArticleText is the article as plain text with a title and date header.
func ArticleText(a *Article) (string, error) {
	md, err := FetchArticleMarkdown(a.MD)
	if err != nil {
		return "", err
	}
	body := MarkdownToText(md)
	var names []string
	for _, id := range a.Authors {
		names = append(names, AuthorName(id))
	}
	line := a.TypeLabel() + ", " + a.Date()
	if len(names) > 0 {
		line += ", by " + strings.Join(names, ", ")
	}
	first, rest, hasRest := strings.Cut(body, "\n")
	if letters(first) == letters(a.Title) {
		body = ""
		if hasRest {
			body = strings.TrimLeft(rest, "\n")
		}
	}
	return a.Title + "\n" + line + "\n\n" + body, nil
}
