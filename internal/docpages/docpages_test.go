package docpages

import (
	"strings"
	"testing"
)

func TestLicenseMarkdownToHTML(t *testing.T) {
	out := MarkdownToHTML("# Title\n\nSome `code` and\nhttps://example.com here.\n\n" +
		"| A | B |\n|---|---|\n| x | **y** |\n")
	for _, want := range []string{
		`<h1 id="title">Title</h1>`,
		`<p>Some <code>code</code> and <a href="https://example.com">https://example.com</a> here.</p>`,
		`<th scope="col">A</th>`,
		"<td><strong>y</strong></td>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "---") {
		t.Fatal(out)
	}
}

func TestMarkdownListsCodeAndLinks(t *testing.T) {
	out := MarkdownToHTML("Intro [site](https://example.com) and *this*.\n\n- one\n- two\n  - inner\n- three\n\n" +
		"1. first\n2. second\n\n```\na <b>\n```\n")
	if !strings.Contains(out, `<a href="https://example.com">site</a>`) || !strings.Contains(out, "<em>this</em>") {
		t.Fatal(out)
	}
	if strings.Count(out, "<ul>") != 2 || !strings.Contains(strings.ReplaceAll(out, "\n", ""), "<li>inner</li></ul>") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "<ol>") || !strings.Contains(out, "<li>second") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "<pre><code>a &lt;b&gt;</code></pre>") {
		t.Fatal(out)
	}
}

func TestEveryDocumentIsBuiltIn(t *testing.T) {
	for i, d := range Documents {
		if strings.Contains(Read(d.FileName, d.Folder), "was not found") {
			t.Fatalf("%d %s missing", i, d.Name)
		}
	}
}
