package chorus

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func fromJSON(t *testing.T, s string) *Chart {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return ChartFromAPI(m)
}

func TestChartFileNameIsSafe(t *testing.T) {
	c := fromJSON(t, `{"md5": "abc", "name": "A/B: \"C\"?", "artist": "D<E>", "charter": "<color=#fff>X</color>"}`)
	if c.Charter != "X" {
		t.Fatal(c.Charter)
	}
	name := c.FileName()
	if !strings.HasSuffix(name, ".sng") || strings.ContainsAny(name, `<>:"/\|?*`) {
		t.Fatal(name)
	}
}

func TestParts(t *testing.T) {
	c := fromJSON(t, `{"md5": "m", "name": "S", "diff_guitar": 4, "diff_drums": 2, "diff_guitarghl": 3, "pro_drums": true,
		"notesData": {"instruments": ["guitar", "drums", "guitarghl"], "noteCounts": [
			{"instrument": "guitar", "difficulty": "expert", "count": 10},
			{"instrument": "guitar", "difficulty": "easy", "count": 3},
			{"instrument": "drums", "difficulty": "hard", "count": 5},
			{"instrument": "guitarghl", "difficulty": "expert", "count": 7}]}}`)
	want := "Guitar, intensity 4: Easy, Expert. Drums, pro, intensity 2: Hard. " +
		"Guitar, 6 fret, intensity 3: Expert (not playable in YARG yet)"
	if got := c.InstrumentsText(); got != want {
		t.Fatal(got)
	}
	if got := fromJSON(t, `{"md5": "m"}`).InstrumentsText(); got != "None listed" {
		t.Fatal(got)
	}
}

func TestPreviewPartsFiveFretAndDrums(t *testing.T) {
	c := fromJSON(t, `{"md5": "m", "name": "S", "diff_guitar": 4, "diff_drums": 2, "diff_bass": 1, "diff_vocals": 3,
		"notesData": {"noteCounts": [
			{"instrument": "guitar", "difficulty": "expert", "count": 10},
			{"instrument": "guitar", "difficulty": "easy", "count": 3},
			{"instrument": "drums", "difficulty": "hard", "count": 5},
			{"instrument": "vocals", "difficulty": "expert", "count": 5}]}}`)
	var got [][2]any
	for _, p := range c.PreviewParts() {
		got = append(got, [2]any{p.Key, p.Difficulties})
	}
	want := [][2]any{{"guitar", []string{"easy", "expert"}}, {"drums", []string{"hard"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func TestSearchBodies(t *testing.T) {
	type sent struct {
		url  string
		body map[string]any
	}
	var calls []sent
	old := postJSON
	postJSON = func(url string, body any, out any) error {
		calls = append(calls, sent{url, body.(map[string]any)})
		return json.Unmarshal([]byte(`{"found": 0, "data": []}`), out)
	}
	t.Cleanup(func() { postJSON = old })

	Search(Filters{Text: "sandman", Instrument: "guitar", Difficulty: "expert", Sort: Sorts[1]}, 2)
	last := calls[len(calls)-1]
	if last.url != SearchURL || last.body["search"] != "sandman" || last.body["page"] != 2 {
		t.Fatal(last)
	}
	if last.body["difficulty"] != "expert" ||
		!reflect.DeepEqual(last.body["sort"], map[string]any{"type": "modifiedTime", "direction": "desc"}) {
		t.Fatal(last)
	}
	Search(Filters{Text: "sandman", Charter: "AlexFrets"}, 1)
	last = calls[len(calls)-1]
	if _, ok := last.body["search"]; last.url != AdvancedURL || ok {
		t.Fatal(last)
	}
	if last.body["charter"].(map[string]any)["value"] != "AlexFrets" || last.body["name"].(map[string]any)["value"] != "sandman" {
		t.Fatal(last)
	}
	Search(Filters{Charter: "AlexFrets"}, 1)
	if _, ok := calls[len(calls)-1].body["name"]; ok {
		t.Fatal("name sent without text")
	}
}

func TestSearchDropsRepeatedCharts(t *testing.T) {
	old := postJSON
	postJSON = func(url string, body any, out any) error {
		return json.Unmarshal([]byte(`{"found": 3, "data": [
			{"md5": "a", "name": "One", "packName": "Pack 1"},
			{"md5": "a", "name": "One", "packName": "Pack 2"},
			{"md5": "b", "name": "Two"}]}`), out)
	}
	t.Cleanup(func() { postJSON = old })
	res, err := Search(Filters{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Charts) != 2 || res.Charts[0].MD5 != "a" || res.Charts[1].MD5 != "b" {
		t.Fatal(res.Charts)
	}
}
