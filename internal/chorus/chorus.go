// Package chorus searches and downloads charts from Chorus Encore (https://www.enchor.us).
//
//	Search:   POST https://api.enchor.us/search  -> {found, data: [chart...]}
//	          POST https://api.enchor.us/search/advanced for field filters such as the charter
//	Download: https://files.enchor.us/<md5>.sng  (YARG reads .sng files directly)
//
// Charts with a video background also have <md5>_novideo.sng, which is much smaller.
package chorus

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/netx"
)

const (
	SearchURL   = "https://api.enchor.us/search"
	AdvancedURL = "https://api.enchor.us/search/advanced"
	FilesURL    = "https://files.enchor.us"
	PageSize    = 25
)

// Option is a filter choice: Value is sent to the API ("" or nil for none), Label is shown.
type Option struct {
	Value string
	Label string
}

var DifficultyFilters = []Option{
	{"", "Any difficulty"},
	{"expert", "Expert"},
	{"hard", "Hard"},
	{"medium", "Medium"},
	{"easy", "Easy"},
}

// Sort is a sort order; Type "" means best match.
type Sort struct {
	Type      string
	Direction string
	Label     string
}

var Sorts = []Sort{
	{"", "", "Best match"},
	{"modifiedTime", "desc", "Newest first"},
	{"name", "asc", "Song name"},
	{"artist", "asc", "Artist"},
	{"charter", "asc", "Charter"},
	{"year", "desc", "Year, newest first"},
	{"length", "asc", "Shortest first"},
	{"length", "desc", "Longest first"},
}

var Instruments = []Option{
	{"", "Any instrument"},
	{"guitar", "Guitar"},
	{"guitarcoop", "Co-op guitar"},
	{"bass", "Bass"},
	{"rhythm", "Rhythm"},
	{"drums", "Drums"},
	{"keys", "Keys"},
	{"vocals", "Vocals"},
	{"guitarghl", "Guitar, 6 fret"},
	{"bassghl", "Bass, 6 fret"},
}

var partDefs = []struct{ key, field, name string }{
	{"guitar", "diff_guitar", "Guitar"},
	{"guitarcoop", "diff_guitar_coop", "Co-op guitar"},
	{"rhythm", "diff_rhythm", "Rhythm"},
	{"bass", "diff_bass", "Bass"},
	{"keys", "diff_keys", "Keys"},
	{"drums", "diff_drums", "Drums"},
	{"vocals", "diff_vocals", "Vocals"},
	{"guitarghl", "diff_guitarghl", "Guitar, 6 fret"},
	{"guitarcoopghl", "diff_guitar_coop_ghl", "Co-op guitar, 6 fret"},
	{"rhythmghl", "diff_rhythm_ghl", "Rhythm, 6 fret"},
	{"bassghl", "diff_bassghl", "Bass, 6 fret"},
}

var sixFret = map[string]bool{"guitarghl": true, "guitarcoopghl": true, "rhythmghl": true, "bassghl": true}
var withSounds = map[string]bool{"guitar": true, "guitarcoop": true, "rhythm": true, "bass": true, "keys": true, "drums": true}

var Difficulties = []string{"easy", "medium", "hard", "expert"}

// Part is one charted instrument: the difficulties it has notes on (easiest first) and its
// intensity, the charter's 0-6 rating from song.ini (-1 when not given).
type Part struct {
	Key          string
	Name         string
	Difficulties []string
	Intensity    int
}

func (p Part) HasSounds() bool { return withSounds[p.Key] && len(p.Difficulties) > 0 }

func Capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func (p Part) Spoken() string {
	text := p.Name
	if p.Intensity >= 0 {
		text += fmt.Sprintf(", intensity %d", p.Intensity)
	}
	if len(p.Difficulties) > 0 {
		var d []string
		for _, x := range p.Difficulties {
			d = append(d, Capitalize(x))
		}
		text += ": " + strings.Join(d, ", ")
	}
	if sixFret[p.Key] {
		text += " (not playable in YARG yet)"
	}
	return text
}

// raw is one chart as the API sends it; fields vary in type, so they are read loosely.
type raw map[string]any

func (d raw) str(k string) string {
	switch v := d[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "True"
		}
		return "False"
	}
	return ""
}

func (d raw) num(k string) int {
	switch v := d[k].(type) {
	case float64:
		return int(v)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return int(f)
	}
	return 0
}

// truthy is Python's bool() of a JSON value.
func (d raw) truthy(k string) bool {
	switch v := d[k].(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != ""
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	}
	return false
}

func (d raw) sub(k string) raw {
	if m, ok := d[k].(map[string]any); ok {
		return raw(m)
	}
	return raw{}
}

func parts(d raw) []Part {
	notes := d.sub("notesData")
	found := map[string]map[string]bool{}
	if counts, ok := notes["noteCounts"].([]any); ok {
		for _, c := range counts {
			nc, ok := c.(map[string]any)
			if !ok {
				continue
			}
			r := raw(nc)
			inst, diff := r.str("instrument"), r.str("difficulty")
			if inst != "" && contains(Difficulties, diff) && r.num("count") > 0 {
				if found[inst] == nil {
					found[inst] = map[string]bool{}
				}
				found[inst][diff] = true
			}
		}
	}
	listed := map[string]bool{}
	if insts, ok := notes["instruments"].([]any); ok {
		for _, i := range insts {
			if s, ok := i.(string); ok {
				listed[s] = true
			}
		}
	}
	var out []Part
	for _, pd := range partDefs {
		intensity := -1
		if v, ok := d[pd.field].(float64); ok && v == float64(int(v)) {
			intensity = int(v)
		}
		if found[pd.key] == nil && !listed[pd.key] && intensity < 0 {
			continue
		}
		name := pd.name
		if pd.key == "drums" {
			if d.truthy("five_lane_drums") {
				name = "Drums, 5 lane"
			} else if d.truthy("pro_drums") {
				name = "Drums, pro"
			}
		}
		var diffs []string
		for _, x := range Difficulties {
			if found[pd.key][x] {
				diffs = append(diffs, x)
			}
		}
		out = append(out, Part{pd.key, name, diffs, intensity})
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

type Chart struct {
	MD5            string
	Name           string
	Artist         string
	Album          string
	Charter        string
	Year           string
	Genre          string
	LengthMs       int
	HasVideo       bool
	Parts          []Part
	Details        string
	PreviewStartMs int
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func ChartFromAPI(m map[string]any) *Chart {
	d := raw(m)
	return &Chart{
		MD5: d.str("md5"), Name: or(d.str("name"), "Unknown song"), Artist: or(d.str("artist"), "Unknown artist"),
		Album: d.str("album"), Charter: stripTags(d.str("charter")), Year: d.str("year"), Genre: d.str("genre"),
		LengthMs: d.num("song_length"), HasVideo: d.truthy("hasVideoBackground"),
		Parts: parts(d), Details: details(d), PreviewStartMs: d.num("preview_start_time"),
	}
}

func (c *Chart) InstrumentsText() string {
	var s []string
	for _, p := range c.Parts {
		s = append(s, p.Spoken())
	}
	if len(s) == 0 {
		return "None listed"
	}
	return strings.Join(s, ". ")
}

// PreviewParts are the parts the preview can play: those with YargAccess note sounds. Chorus does not
// list pro keys, so a chart with keys also offers them; the preview says so when the chart has none.
func (c *Chart) PreviewParts() []Part {
	var out []Part
	keys := false
	for _, p := range c.Parts {
		if p.HasSounds() {
			out = append(out, p)
			keys = keys || p.Key == "keys"
		}
	}
	if keys {
		out = append(out, Part{"prokeys", "Pro keys, if the chart has them", append([]string(nil), Difficulties...), -1})
	}
	return out
}

func (c *Chart) LengthText() string {
	if c.LengthMs <= 0 {
		return ""
	}
	s := c.LengthMs / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func (c *Chart) Spoken() string {
	p := []string{c.Name + " by " + c.Artist}
	if c.LengthText() != "" {
		p = append(p, c.LengthText())
	}
	if c.Charter != "" {
		p = append(p, "charted by "+c.Charter)
	}
	if len(c.Parts) > 0 {
		p = append(p, c.InstrumentsText())
	}
	return strings.Join(p, ". ")
}

var badName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func (c *Chart) FileName() string {
	base := fmt.Sprintf("%s - %s (%s)", c.Artist, c.Name, or(c.Charter, "unknown"))
	base = strings.Trim(badName.ReplaceAllString(base, "_"), " .")
	if r := []rune(base); len(r) > 150 {
		base = string(r[:150])
	}
	return base + ".sng"
}

var spaces = regexp.MustCompile(`\s+`)

// details joins what Chorus has instead of a description: album, year, genre, chart features, and
// the loading phrase charters write (often a description or trivia).
func details(d raw) string {
	notes := d.sub("notesData")
	var parts []string
	album, year := d.str("album"), strings.TrimSpace(d.str("year"))
	if album != "" {
		s := "Album " + album
		if year != "" {
			s += ", " + year
		}
		parts = append(parts, s)
	} else if year != "" {
		parts = append(parts, year)
	}
	if g := d.str("genre"); g != "" {
		parts = append(parts, g)
	}
	var features []string
	if notes.truthy("hasLyrics") || notes.truthy("hasVocals") {
		features = append(features, "lyrics")
	}
	if d.truthy("pro_drums") {
		features = append(features, "pro drums")
	}
	if d.truthy("five_lane_drums") {
		features = append(features, "5-lane drums")
	}
	if notes.truthy("hasSoloSections") {
		features = append(features, "solo sections")
	}
	if d.truthy("hasVideoBackground") {
		features = append(features, "video background")
	}
	if d.truthy("modchart") {
		features = append(features, "modchart")
	}
	if len(features) > 0 {
		parts = append(parts, "Has "+strings.Join(features, ", "))
	}
	if phrase := strings.TrimSpace(spaces.ReplaceAllString(stripTags(d.str("loading_phrase")), " ")); phrase != "" {
		parts = append(parts, phrase)
	}
	for i := range parts {
		parts[i] = strings.TrimRight(parts[i], ".")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ". ") + "."
}

var tags = regexp.MustCompile(`<[^>]+>`)

func stripTags(s string) string { return tags.ReplaceAllString(s, "") }

type SearchResult struct {
	Found  int
	Page   int
	Charts []*Chart
}

// Filters are a search's settings.
type Filters struct {
	Text       string
	Instrument string
	Difficulty string
	Sort       Sort
	Charter    string
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// postJSON is a variable so tests can see what a search sends.
var postJSON = netx.PostJSON

// Body is the JSON body and the address a search posts to.
func Body(f Filters, page, perPage int) (string, map[string]any) {
	vocals := f.Instrument == "vocals"
	instrument := nullable(f.Instrument)
	if vocals {
		instrument = nil
	}
	var sort any
	if f.Sort.Type != "" {
		sort = map[string]any{"type": f.Sort.Type, "direction": f.Sort.Direction}
	}
	body := map[string]any{
		"page": page, "per_page": perPage, "instrument": instrument, "difficulty": nullable(f.Difficulty),
		"drumType": nil, "drumsReviewed": false, "sort": sort, "source": "website",
	}
	charter, text := strings.TrimSpace(f.Charter), strings.TrimSpace(f.Text)
	if charter != "" || vocals {
		if charter != "" {
			body["charter"] = map[string]any{"value": charter, "exact": false, "exclude": false}
		}
		if vocals {
			body["hasVocals"] = true
		}
		if text != "" {
			body["name"] = map[string]any{"value": text, "exact": false, "exclude": false}
		}
		return AdvancedURL, body
	}
	body["search"] = f.Text
	return SearchURL, body
}

// Search finds charts. Text may be empty: the filters alone pick the charts. The charter and vocals
// filters are part of the API's advanced search, which has no "any field" search, so with either the
// text matches song names.
func Search(f Filters, page int) (*SearchResult, error) {
	url, body := Body(f, page, PageSize)
	var data struct {
		Found any              `json:"found"`
		Data  []map[string]any `json:"data"`
	}
	if err := postJSON(url, body, &data); err != nil {
		return nil, err
	}
	res := &SearchResult{Found: raw{"f": data.Found}.num("f"), Page: page}
	// The API sends one row per Drive pack a chart is in, so the same file (same md5) can come
	// several times on a page. Keep the first.
	seen := map[string]bool{}
	for _, d := range data.Data {
		c := ChartFromAPI(d)
		if c.MD5 != "" && seen[c.MD5] {
			continue
		}
		seen[c.MD5] = true
		res.Charts = append(res.Charts, c)
	}
	return res, nil
}

// Download saves the chart. skipVideo gets the smaller copy without the background video when the
// chart has one.
func Download(c *Chart, folder string, progress netx.Progress, cancel <-chan struct{}, skipVideo bool) (string, error) {
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(folder, c.FileName())
	if skipVideo && c.HasVideo {
		p, err := netx.Download(FilesURL+"/"+c.MD5+"_novideo.sng", dest, progress, cancel, nil)
		if err == nil || !netx.IsStatus(err, 404) {
			return p, err
		}
	}
	return netx.Download(FilesURL+"/"+c.MD5+".sng", dest, progress, cancel, nil)
}
