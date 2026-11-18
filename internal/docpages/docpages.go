// Package docpages holds the documents on the Documents tab and the Markdown to HTML conversion they use.
//
// The mod's changelog, contributing guide, licence and third-party notices sit in data/mod, copied from the mod
// repository by tools/sync-mod-docs.ps1. The launcher's own licence files sit in the repository root.
// All of them are built into the exe.
//
// Pages are plain HTML with headings, lists and real tables, which a screen reader reads in browse
// mode (H moves by heading, L by list, T by table).
package docpages

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	launcher "github.com/MichaelJohann1/YargAccessLauncher"
)

type Document struct {
	Name     string
	FileName string
	Folder   string // "mod" or "root"
}

var Documents = []Document{
	{"Read me: using the launcher, getting songs and getting help", "README.md", "root"},
	{"Contributing: running, building and sending changes", "CONTRIBUTING.md", "mod"},
	{"YargAccess mod: changelog", "CHANGELOG.md", "mod"},
	{"YARG Access launcher: changelog", "CHANGELOG.md", "root"},
	{"YargAccess mod: licence (GNU General Public License version 3)", "LICENSE", "mod"},
	{"YargAccess mod: third-party licences and notices", "THIRD-PARTY-LICENSES.md", "mod"},
	{"YARG Access launcher: licence (GNU General Public License version 3)", "LICENSE", "root"},
	{"YARG Access launcher: third-party licences and notices", "THIRD-PARTY-LICENSES.md", "root"},
}

const style = `
:root { color-scheme: light dark; --fg: #1b1b1b; --bg: #ffffff; --muted: #5a5a5a; --line: #d0d0d0;
        --head: #f2f2f2; --link: #0b57d0; --code: #f4f4f4; }
@media (prefers-color-scheme: dark) {
  :root { --fg: #e8e8e8; --bg: #1e1e1e; --muted: #b0b0b0; --line: #444; --head: #2a2a2a;
          --link: #8ab4f8; --code: #2b2b2b; }
}
body { font: 15px/1.55 "Segoe UI", system-ui, sans-serif; color: var(--fg); background: var(--bg);
       margin: 0; padding: 16px 20px 32px; max-width: 60em; }
h1 { font-size: 1.5em; margin: 0 0 .6em; }
h2 { font-size: 1.2em; margin: 1.6em 0 .5em; padding-bottom: .2em; border-bottom: 1px solid var(--line); }
h3 { font-size: 1.05em; margin: 1.2em 0 .4em; }
p, li { margin: .4em 0; }
a { color: var(--link); }
code { font: .92em Consolas, monospace; background: var(--code); padding: 0 .25em; border-radius: 3px; }
pre { font: 13px/1.45 Consolas, monospace; white-space: pre-wrap; background: var(--code); padding: .6em .8em; }
pre.plain { background: none; padding: 0; margin: 0; }
table { border-collapse: collapse; width: 100%; margin: .8em 0; }
th, td { border: 1px solid var(--line); padding: .35em .6em; text-align: left; vertical-align: top; }
th { background: var(--head); }
`

// Read returns a document's text.
func Read(fileName, folder string) string {
	path := fileName
	if folder == "mod" {
		path = "data/mod/" + fileName
	}
	b, err := launcher.Files.ReadFile(path)
	if err != nil {
		return fileName + " was not found next to the launcher."
	}
	return string(b)
}

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

var (
	codeSpan  = regexp.MustCompile("`([^`]+)`")
	mdLink    = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	bareURL   = regexp.MustCompile(`https?://[^\s<|)]+`)
	boldRx    = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	codeToken = regexp.MustCompile("\x00(\\d+)\x00")
)

func isWord(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}

// linkBare turns https:// addresses into links unless they follow ", ', > or = (already in a tag).
func linkBare(s string) string {
	var out strings.Builder
	last := 0
	for _, m := range bareURL.FindAllStringIndex(s, -1) {
		if m[0] > 0 && strings.IndexByte(`"'>=`, s[m[0]-1]) >= 0 {
			continue
		}
		out.WriteString(s[last:m[0]])
		u := s[m[0]:m[1]]
		out.WriteString(`<a href="` + u + `">` + u + `</a>`)
		last = m[1]
	}
	out.WriteString(s[last:])
	return out.String()
}

// italics is (?<![*\w])\*([^*\s][^*]*)\*(?![*\w]) without lookarounds.
func italics(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '*' && (i == 0 || !(isWord(s[i-1]) || s[i-1] == '*')) && i+1 < len(s) &&
			s[i+1] != '*' && s[i+1] != ' ' && s[i+1] != '\t' && s[i+1] != '\n' {
			// [^*]* is greedy but cannot cross a *, so the only candidate close is the next *.
			if j := strings.IndexByte(s[i+1:], '*'); j >= 0 {
				j += i + 1
				if j+1 == len(s) || !(isWord(s[j+1]) || s[j+1] == '*') {
					out.WriteString("<em>" + s[i+1:j] + "</em>")
					i = j + 1
					continue
				}
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// inline escapes text and turns `code`, **bold**, *italic*, [links](url) and bare https:// links into HTML.
func inline(text string) string {
	var codes []string
	out := codeSpan.ReplaceAllStringFunc(text, func(m string) string {
		codes = append(codes, "<code>"+esc(codeSpan.FindStringSubmatch(m)[1])+"</code>")
		return fmt.Sprintf("\x00%d\x00", len(codes)-1)
	})
	out = esc(out)
	out = mdLink.ReplaceAllStringFunc(out, func(m string) string {
		g := mdLink.FindStringSubmatch(m)
		return `<a href="` + html.EscapeString(g[2]) + `">` + g[1] + `</a>`
	})
	out = linkBare(out)
	out = boldRx.ReplaceAllString(out, "<strong>$1</strong>")
	out = italics(out)
	return codeToken.ReplaceAllStringFunc(out, func(m string) string {
		n, _ := strconv.Atoi(codeToken.FindStringSubmatch(m)[1])
		return codes[n]
	})
}

var (
	slugStrip = regexp.MustCompile("[`*_\\[\\]()]")
	slugKeep  = regexp.MustCompile(`[^\p{L}\p{N}_\- ]`)
)

// Slug is a heading's anchor, made the way GitHub makes it, so a README's contents links work here too.
func Slug(heading string) string {
	text := strings.ToLower(strings.TrimSpace(slugStrip.ReplaceAllString(heading, "")))
	return strings.ReplaceAll(slugKeep.ReplaceAllString(text, ""), " ", "-")
}

var (
	sepCell  = regexp.MustCompile(`^:?-+:?$`)
	listItem = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)
	headRx   = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	ruleRx   = regexp.MustCompile(`^(-{3,}|\*{3,}|_{3,})$`)
)

type openList struct {
	tag    string
	indent int
}

// MarkdownToHTML converts GitHub-style Markdown as the documents use it: headings, paragraphs,
// bullet and numbered lists (one level of nesting), fenced code blocks, pipe tables, block quotes and rules.
func MarkdownToHTML(md string) string {
	var parts, para, code []string
	var rows [][]string
	var lists []openList
	inCode := false

	flushPara := func() {
		if len(para) > 0 {
			parts = append(parts, "<p>"+inline(strings.Join(para, " "))+"</p>")
			para = nil
		}
	}
	flushTable := func() {
		if len(rows) == 0 {
			return
		}
		var t strings.Builder
		t.WriteString("<table><thead><tr>")
		for _, c := range rows[0] {
			t.WriteString(`<th scope="col">` + inline(c) + "</th>")
		}
		t.WriteString("</tr></thead><tbody>")
		for _, r := range rows[1:] {
			t.WriteString("<tr>")
			for _, c := range r {
				t.WriteString("<td>" + inline(c) + "</td>")
			}
			t.WriteString("</tr>")
		}
		t.WriteString("</tbody></table>")
		parts = append(parts, t.String())
		rows = nil
	}
	closeLists := func(toIndent int) {
		for len(lists) > 0 && lists[len(lists)-1].indent > toIndent {
			parts = append(parts, "</li></"+lists[len(lists)-1].tag+">")
			lists = lists[:len(lists)-1]
		}
	}

	for _, line := range splitLines(md) {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "```") {
			if inCode {
				parts = append(parts, "<pre><code>"+html.EscapeString(strings.Join(code, "\n"))+"</code></pre>")
				code = nil
				inCode = false
			} else {
				flushPara()
				flushTable()
				closeLists(-1)
				inCode = true
			}
			continue
		}
		if inCode {
			code = append(code, line)
			continue
		}
		if strings.HasPrefix(stripped, "|") {
			flushPara()
			closeLists(-1)
			cells := strings.Split(strings.Trim(stripped, "|"), "|")
			sep := true
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
				if !sepCell.MatchString(cells[i]) {
					sep = false
				}
			}
			if !sep {
				rows = append(rows, cells)
			}
			continue
		}
		flushTable()
		if item := listItem.FindStringSubmatch(line); item != nil {
			flushPara()
			indent := len(strings.ReplaceAll(item[1], "\t", "    "))
			tag := "ol"
			if strings.Contains("-*+", item[2]) {
				tag = "ul"
			}
			if len(lists) > 0 && indent > lists[len(lists)-1].indent {
				parts = append(parts, "<"+tag+">")
				lists = append(lists, openList{tag, indent})
			} else {
				closeLists(indent)
				if len(lists) > 0 && lists[len(lists)-1].indent == indent {
					parts = append(parts, "</li>")
				} else {
					parts = append(parts, "<"+tag+">")
					lists = append(lists, openList{tag, indent})
				}
			}
			parts = append(parts, "<li>"+inline(item[3]))
			continue
		}
		if len(lists) > 0 && stripped != "" && (line[0] == ' ' || line[0] == '\t') {
			parts = append(parts, " "+inline(stripped))
			continue
		}
		closeLists(-1)
		switch h := headRx.FindStringSubmatch(stripped); {
		case h != nil:
			flushPara()
			level := len(h[1])
			parts = append(parts, fmt.Sprintf(`<h%d id="%s">%s</h%d>`, level, Slug(h[2]), inline(h[2]), level))
		case ruleRx.MatchString(stripped):
			flushPara()
			parts = append(parts, "<hr>")
		case strings.HasPrefix(stripped, ">"):
			flushPara()
			parts = append(parts, "<blockquote><p>"+inline(strings.TrimLeft(stripped, "> "))+"</p></blockquote>")
		case stripped == "":
			flushPara()
		default:
			para = append(para, stripped)
		}
	}
	if inCode {
		parts = append(parts, "<pre><code>"+html.EscapeString(strings.Join(code, "\n"))+"</code></pre>")
	}
	flushPara()
	flushTable()
	closeLists(-1)
	return strings.Join(parts, "\n")
}

// splitLines is Python's str.splitlines for \n, \r\n and \r.
func splitLines(s string) []string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// pageScript opens web links in the default browser, through WebView2's postMessage. (Escape is
// caught by the launcher as a WebView2 accelerator key.)
const pageScript = `<script>(function () {
  var host = window.chrome && window.chrome.webview;
  if (!host) { return; }
  document.addEventListener('click', function (e) {
    var a = e.target && e.target.closest ? e.target.closest('a[href]') : null;
    if (a && /^https?:/i.test(a.href)) { e.preventDefault(); host.postMessage('open:' + a.href); }
  });
})();</script>`

// Page is a whole HTML page. With script, links open in the default browser.
func Page(title, body string, script bool) string {
	s := ""
	if script {
		s = pageScript
	}
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" ` +
		`content="width=device-width, initial-scale=1"><title>` + html.EscapeString(title) + `</title>` +
		`<style>` + style + `</style></head><body>` + body + s + `</body></html>`
}

// DocumentPage is the HTML for Documents[index]: an .html file as it is, Markdown converted, plain
// text in <pre>.
func DocumentPage(index int, script bool) string {
	d := Documents[index]
	text := Read(d.FileName, d.Folder)
	lower := strings.ToLower(d.FileName)
	switch {
	case strings.HasSuffix(lower, ".html"):
		if script && strings.Contains(text, "</body>") {
			return strings.Replace(text, "</body>", pageScript+"</body>", 1)
		}
		return text
	case strings.HasSuffix(lower, ".md"):
		return Page(d.Name, MarkdownToHTML(text), script)
	}
	return Page(d.Name, "<h1>"+html.EscapeString(d.Name)+`</h1><pre class="plain">`+html.EscapeString(text)+"</pre>", script)
}
