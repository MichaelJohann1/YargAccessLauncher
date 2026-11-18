package ui

import (
	"fmt"

	"github.com/lxn/walk"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/news"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/speech"
)

// News tab: YARG announcements and release notes, newest first.
type NewsPage struct {
	w        *MainWindow
	articles []*news.Article
	loaded   bool
	list     *ReportList
	article  *walk.TextEdit
}

func newNewsPage(w *MainWindow) *NewsPage {
	p := &NewsPage{w: w}
	tab := w.newPage("News")
	label(tab, "News:")
	p.list = NewReportList(tab, []Column{{"Title", 420}, {"Type", 130}, {"Date", 110}}, "News")
	p.list.OnActivated(p.OpenArticle)

	r := row(tab)
	button(r, "&Read article", p.OpenArticle)
	button(r, "Re&load news", p.Load)
	hspacer(r)

	label(tab, "Article:")
	p.article = readOnlyText(tab, "Article")
	wrapLabel(tab, "Enter on a headline opens the article below. Escape closes the article and goes back to the list.")

	l := tab.Layout().(*walk.BoxLayout)
	l.SetStretchFactor(p.list, 1)
	l.SetStretchFactor(p.article, 2)
	return p
}

func (p *NewsPage) OnShown() {
	if !p.loaded {
		p.Load()
	}
}

func (p *NewsPage) Load() {
	p.loaded = true
	p.w.announce("Loading news.", false)
	go func() {
		articles, err := news.FetchIndex()
		p.w.Synchronize(func() {
			if err != nil {
				p.failed(fmt.Sprintf("Could not load the news.\n\n%v", err))
				return
			}
			p.show(articles)
		})
	}()
}

func (p *NewsPage) failed(message string) {
	p.loaded = false
	errorBox(p.w, message, "Error")
}

func (p *NewsPage) show(articles []*news.Article) {
	p.articles = articles
	p.list.DeleteAll()
	for _, a := range articles {
		p.list.Add(a.Title, a.TypeLabel(), a.Date())
	}
	if len(articles) > 0 {
		p.list.FocusRow(0)
		p.w.announce(fmt.Sprintf("%d news articles. Newest: %s, %s.", len(articles), articles[0].Title, articles[0].Date()), false)
	}
}

func (p *NewsPage) OpenArticle() {
	i := p.list.Selected()
	if i < 0 || i >= len(p.articles) {
		speech.Speak("Select an article first.", true)
		return
	}
	a := p.articles[i]
	speech.Speak("Opening article.", true)
	go func() {
		text, err := news.ArticleText(a)
		p.w.Synchronize(func() {
			if err != nil {
				errorBox(p.w, fmt.Sprintf("Could not open the article.\n\n%v", err), "Error")
				return
			}
			p.article.SetText(crlf(text))
			p.article.SetTextSelection(0, 0)
			p.article.SetFocus()
		})
	}()
}

func (p *NewsPage) CloseArticle() {
	p.article.SetText("")
	i := p.list.Selected()
	if i < 0 {
		i = 0
	}
	p.list.FocusRow(i)
	p.list.SetFocus()
	speech.Speak("Article closed.", true)
}
