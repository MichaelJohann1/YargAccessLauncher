package internal_test

// Live checks against the real servers, YARG's BASS and local charts. They run only with
// YARGACCESS_LIVE=1 (and YARGACCESS_7Z=<folder with enc.7z and plain.7z> for the archive check).

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/archives"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/bass"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/catalog"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/chorus"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/config"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/installer"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/minisign"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/netx"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/news"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/preview"
)

func live(t *testing.T) {
	if os.Getenv("YARGACCESS_LIVE") == "" {
		t.Skip("set YARGACCESS_LIVE=1")
	}
}

func TestLiveCatalog(t *testing.T) {
	live(t)
	entries, err := catalog.FetchCatalog()
	if err != nil || len(entries) == 0 {
		t.Fatal(err)
	}
	t.Logf("%d catalog entries", len(entries))
	for _, url := range []string{catalog.GameProfileURL("stable"), catalog.GameProfileURL("nightly"), catalog.OfficialSetlistEntry().URL} {
		info, err := catalog.FetchProfile(url)
		if err != nil {
			t.Fatal(url, err)
		}
		t.Logf("%s: %s %s, %d files, %d songs, exe %s, first %+v", url, info.Name, info.Release.Tag,
			len(info.Release.Files), len(info.Songs), info.Release.Executable, info.Release.Files[0])
	}
	n := 0
	installer.FetchProfiles(entries[:6], func(e *catalog.Entry, info *catalog.ProfileInfo) {
		if info != nil {
			n++
		}
	}, 8)
	t.Logf("%d of 6 profiles fetched", n)
}

func TestLiveNews(t *testing.T) {
	live(t)
	articles, err := news.FetchIndex()
	if err != nil || len(articles) == 0 {
		t.Fatal(err)
	}
	text, err := news.ArticleText(articles[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > 300 {
		text = text[:300]
	}
	t.Logf("%d articles; newest %s %s %s\n%s", len(articles), articles[0].Title, articles[0].TypeLabel(), articles[0].Date(), text)
}

func TestLiveChorus(t *testing.T) {
	live(t)
	res, err := chorus.Search(chorus.Filters{Text: "sandman"}, 1)
	if err != nil || len(res.Charts) == 0 {
		t.Fatal(err)
	}
	t.Logf("found %d; first: %s | %s", res.Found, res.Charts[0].Spoken(), res.Charts[0].Details)
	res, err = chorus.Search(chorus.Filters{Instrument: "vocals", Charter: "Harmonix", Sort: chorus.Sorts[1]}, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("advanced search found %d", res.Found)
}

func TestLivePreview(t *testing.T) {
	live(t)
	cfg := config.Load()
	files, _ := filepath.Glob(filepath.Join(cfg.ResolvedSongsDir(), "*.sng"))
	if len(files) == 0 {
		t.Skip("no local charts")
	}
	for _, part := range []string{"guitar", "drums"} {
		p, err := preview.Prepare(files[0], part, "expert", cfg.ResolvedGameDir(), 0, true)
		if err != nil {
			t.Logf("%s: %v", part, err)
			continue
		}
		t.Logf("%s: %s, start %s, %d stems, delay %.3f", part, p.Summary, preview.TimeText(p.Start), len(p.Stems), p.Delay)
		player, err := preview.Play(p, cfg.ResolvedGameDir(), 0.05)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(700 * time.Millisecond)
		pos, state, stems := player.Position(), player.State(), player.StemCount()
		player.Seek(p.Start + 30)
		time.Sleep(200 * time.Millisecond)
		after := player.Position()
		bass.StopCurrent()
		t.Logf("length %s, %d stems, state %s, position %.2f after start %.2f, after seek %.2f", preview.TimeText(player.Length),
			stems, state, pos, p.Start, after)
		if state != "playing" || pos <= p.Start || after < p.Start+30 || stems != len(p.Stems) {
			t.Fatal("playback did not advance")
		}
	}
}

func TestLive7z(t *testing.T) {
	dir := os.Getenv("YARGACCESS_7Z")
	if dir == "" {
		t.Skip("set YARGACCESS_7Z")
	}
	for name, kind := range map[string]string{"enc.7z": "encrypted", "plain.7z": "7z"} {
		out := t.TempDir()
		if err := archives.Extract(filepath.Join(dir, name), out, kind); err != nil {
			t.Fatal(name, err)
		}
		st, err := os.Stat(filepath.Join(out, "src", "sub", "b.bin"))
		if err != nil || st.Size() != 300000 {
			t.Fatal(name, err)
		}
	}
}

func TestLiveDownloadCancel(t *testing.T) {
	live(t)
	dest := filepath.Join(t.TempDir(), "yarg.zip")
	cancel := make(chan struct{})
	var seen int64
	time.AfterFunc(1500*time.Millisecond, func() { close(cancel) })
	_, err := netx.Download("https://github.com/YARC-Official/YARG/releases/download/v0.15.0/YARG_v0.15.0-Windows-x64.zip",
		dest, func(done, total int64) { seen = done }, cancel, nil)
	if !errors.Is(err, netx.ErrCancelled) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("dest written")
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Fatal(".part left")
	}
	t.Logf("cancelled after %d bytes", seen)
}

func TestLiveYargSignatureAndUnpack(t *testing.T) {
	if os.Getenv("YARGACCESS_LIVE_BIG") == "" {
		t.Skip("set YARGACCESS_LIVE_BIG=1 (downloads YARG)")
	}
	dir := t.TempDir()
	url := "https://github.com/YARC-Official/YARG/releases/download/v0.15.0/YARG_v0.15.0-Windows-x64.zip"
	zipPath, err := netx.Download(url, filepath.Join(dir, "yarg.zip"), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := netx.GetBytes(url+".sig", nil)
	if err != nil {
		t.Fatal(err)
	}
	comment, err := minisign.VerifyFile(zipPath, string(sig), minisign.YargPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(zipPath, os.O_RDWR, 0)
	f.WriteAt([]byte{0}, 1000)
	f.Close()
	if _, err := minisign.VerifyFile(zipPath, string(sig), minisign.YargPublicKey); err == nil {
		t.Fatal("damaged file accepted")
	}
	t.Logf("signature ok: %s", comment)
	zipPath, _ = netx.Download(url, filepath.Join(dir, "yarg2.zip"), nil, nil, nil)
	out := filepath.Join(dir, "out")
	if err := archives.Extract(zipPath, out, "zip"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "YARG.exe")); err != nil {
		t.Fatal(err)
	}
}
