package scraper

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// saveCandidatesHTML mirrors how 100% saves appear in a thread's first post:
// an attachment inside a code block, image/media attachments, external
// file-host links with and without save signals, a duplicate, an unrecognised
// host, and an online-only link.
const saveCandidatesHTML = `<div class="bbWrapper">
  <p>Overview</p>
  <div class="bbCodeBlock">
    <p>Full save attached below.</p>
    <a href="https://f95zone.to/attachments/MyGame-Full-Save.482910/">MyGame-Full-Save.482910</a>
  </div>
  <ul>
    <li><a href="https://f95zone.to/attachments/screenshot_001.png.98765/">screenshot</a></li>
    <li><a href="https://mega.nz/file/abc123">100% save</a></li>
    <li><a href="https://mega.nz/file/xyz789">Random extra build</a></li>
    <li><a href="https://example.com/download/whatever">another save</a></li>
    <li><a href="https://f95zone.to/attachments/MyGame-Full-Save.482910/">duplicate link</a></li>
    <li><a href="https://f95zone.to/threads/online-version.5/">Online version</a></li>
  </ul>
</div>`

// selectInBBWrapper parses an HTML fragment and returns its bbWrapper element,
// the same scope parseThreadHTML passes to the extractors.
func selectInBBWrapper(t *testing.T, html string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse test HTML: %v", err)
	}
	sel := doc.Find("div.bbWrapper")
	if sel.Length() == 0 {
		t.Fatal("test HTML has no div.bbWrapper")
	}
	return sel
}

func TestExtractSaveCandidates_AttachmentAndExternal(t *testing.T) {
	t.Parallel()

	got := extractSaveCandidates(selectInBBWrapper(t, saveCandidatesHTML))

	// Only the archive attachment and the save-signalled MEGA link survive.
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2: %+v", len(got), got)
	}

	att := got[0]
	if att.Kind != SaveCandidateAttachment {
		t.Errorf("Kind = %q, want %q", att.Kind, SaveCandidateAttachment)
	}
	if att.Host != "f95zone" {
		t.Errorf("Host = %q, want %q", att.Host, "f95zone")
	}
	if att.URL != "https://f95zone.to/attachments/MyGame-Full-Save.482910/" {
		t.Errorf("URL = %q", att.URL)
	}
	if att.Filename != "MyGame-Full-Save" {
		t.Errorf("Filename = %q, want %q", att.Filename, "MyGame-Full-Save")
	}
	if !strings.Contains(att.Context, "Full save attached below.") {
		t.Errorf("Context = %q, want the enclosing bbCodeBlock text", att.Context)
	}

	ext := got[1]
	if ext.Kind != SaveCandidateExternal {
		t.Errorf("Kind = %q, want %q", ext.Kind, SaveCandidateExternal)
	}
	if ext.Host != "mega" {
		t.Errorf("Host = %q, want %q", ext.Host, "mega")
	}
	if ext.URL != "https://mega.nz/file/abc123" {
		t.Errorf("URL = %q", ext.URL)
	}
	if ext.Filename != "abc123" {
		t.Errorf("Filename = %q, want %q", ext.Filename, "abc123")
	}
	if ext.Context != "100% save" {
		t.Errorf("Context = %q, want %q", ext.Context, "100% save")
	}
}

func TestExtractSaveCandidates_Bare100PercentSignals(t *testing.T) {
	t.Parallel()

	html := `<div class="bbWrapper"><ul>
	  <li><a href="https://mega.nz/file/abc123">MyGame 100%</a></li>
	  <li><a href="https://pixeldrain.com/u/100%25-final">Download</a></li>
	  <li><a href="https://gofile.io/d/nosignal">download</a></li>
	</ul></div>`

	got := extractSaveCandidates(selectInBBWrapper(t, html))
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2: %+v", len(got), got)
	}
	if got[0].Host != "mega" || got[1].Host != "pixeldrain" {
		t.Errorf("hosts = %q/%q, want mega/pixeldrain", got[0].Host, got[1].Host)
	}
}

func TestExtractSaveCandidates_MaskedRedirectUsesLinkText(t *testing.T) {
	t.Parallel()

	html := `<div class="bbWrapper"><div class="bbCodeBlock">
	  <a href="https://f95zone.to/masked/mega.nz/32839/6265512/c_qxN4WgZZgfYMQkJHeiCp2302k/qw4Mqsb3xLpLMLmCeEe.iw/NALRYNPtoken">Full Save</a>
	</div></div>`

	got := extractSaveCandidates(selectInBBWrapper(t, html))
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(got), got)
	}
	if got[0].Filename != "Full Save" {
		t.Errorf("Filename = %q, want the link text %q", got[0].Filename, "Full Save")
	}
	if got[0].Host != "mega" {
		t.Errorf("Host = %q, want mega", got[0].Host)
	}
}

// TestExtractSaveCandidates_RejectsTextHostMismatch proves an external link is
// classified from its href, not its (attacker-controlled) text: a foreign URL
// whose text claims a trusted host must not become a save candidate.
func TestExtractSaveCandidates_RejectsTextHostMismatch(t *testing.T) {
	t.Parallel()

	html := `<div class="bbWrapper"><ul>
	  <li><a href="https://attacker.example/masked/x">100% save gofile</a></li>
	  <li><a href="https://attacker.example/full-save-archive">Download</a></li>
	</ul></div>`

	got := extractSaveCandidates(selectInBBWrapper(t, html))
	if len(got) != 0 {
		t.Fatalf("got %d candidates, want 0 (host must come from the href): %+v", len(got), got)
	}
}

func TestExtractSaveCandidates_SkipsMediaUnsignalledAndOnlineOnly(t *testing.T) {
	t.Parallel()

	got := extractSaveCandidates(selectInBBWrapper(t, saveCandidatesHTML))

	rejected := []string{"screenshot_001", "xyz789", "example.com", "online-version"}
	for _, c := range got {
		for _, r := range rejected {
			if strings.Contains(c.URL, r) {
				t.Errorf("candidate %q should have been skipped", c.URL)
			}
		}
	}
}

func TestExtractSaveCandidates_DedupesByURL(t *testing.T) {
	t.Parallel()

	html := `<div class="bbWrapper"><ul>
	  <li><a href="https://f95zone.to/attachments/save.1/">first</a></li>
	  <li><a href="https://f95zone.to/attachments/save.1/">second</a></li>
	</ul></div>`

	got := extractSaveCandidates(selectInBBWrapper(t, html))
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(got), got)
	}
	if got[0].Context != "first" {
		t.Errorf("Context = %q, want the first occurrence %q", got[0].Context, "first")
	}
}

func TestExtractSaveCandidates_AttachmentURLForms(t *testing.T) {
	t.Parallel()

	html := `<div class="bbWrapper">
	  <a href="https://f95zone.to/attachments/My%20Game%20Save.12345">no trailing slash</a>
	  <a href="https://f95zone.to/attachments/Save-Pack.zip.222/?utm_source=x#top">query ignored</a>
	  <a href="https://attachments.f95zone.to/attachments/Alt-Save.333/">alt host</a>
	  <a href="https://f95zone.to/attachments/no-id/">no attachment id</a>
	  <a href="https://f95zone.to/threads/some-thread.999/">thread link</a>
	  <a href="https://example.com/attachments/save.1/">foreign host</a>
	</div>`

	got := extractSaveCandidates(selectInBBWrapper(t, html))

	want := []string{"My Game Save", "Save-Pack.zip", "Alt-Save"}
	if len(got) != len(want) {
		t.Fatalf("got %d candidates, want %d: %+v", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i].Filename != name {
			t.Errorf("candidate %d Filename = %q, want %q", i, got[i].Filename, name)
		}
		if got[i].Kind != SaveCandidateAttachment || got[i].Host != "f95zone" {
			t.Errorf("candidate %d = %q/%q, want attachment/f95zone", i, got[i].Kind, got[i].Host)
		}
	}
}

func TestExtractSaveCandidates_ExternalSignalFromURL(t *testing.T) {
	t.Parallel()

	html := `<div class="bbWrapper"><ul>
	  <li><a href="https://pixeldrain.com/u/full-save-archive">Download</a></li>
	  <li><a href="https://drive.google.com/file/d/abc/view">Download</a></li>
	</ul></div>`

	got := extractSaveCandidates(selectInBBWrapper(t, html))
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(got), got)
	}
	if got[0].Host != "pixeldrain" {
		t.Errorf("Host = %q, want %q", got[0].Host, "pixeldrain")
	}
	if got[0].Filename != "full-save-archive" {
		t.Errorf("Filename = %q, want %q", got[0].Filename, "full-save-archive")
	}
}

func TestSaveCandidateContext_Fallbacks(t *testing.T) {
	t.Parallel()

	html := `<div class="bbWrapper">
  <ul><li>Download the <a href="https://mega.nz/file/a">full save</a> here</li></ul>
  <div class="footnote">See <a href="https://mega.nz/file/b">this post</a> for details</div>
  <div><a href="https://mega.nz/file/c">100% save</a></div>
</div>`

	anchors := selectInBBWrapper(t, html).Find("a[href]")
	if anchors.Length() != 3 {
		t.Fatalf("got %d anchors, want 3", anchors.Length())
	}

	tests := []struct {
		name string
		idx  int
		want string
	}{
		{"closest li", 0, "Download the full save here"},
		{"parent element", 1, "See this post for details"},
		{"link-only container", 2, "100% save"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := saveCandidateContext(anchors.Eq(tt.idx)); got != tt.want {
				t.Errorf("saveCandidateContext = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSaveCandidateContext_CollapsesAndTruncates(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("word ", 100) // 500 runes before the link
	html := `<div class="bbWrapper"><ul><li>` + long +
		`<a href="https://mega.nz/file/a">100% save</a></li></ul></div>`

	got := extractSaveCandidates(selectInBBWrapper(t, html))
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(got), got)
	}

	ctx := got[0].Context
	if strings.Contains(ctx, "  ") {
		t.Errorf("whitespace not collapsed: %q", ctx)
	}
	if !strings.HasPrefix(ctx, "word word") {
		t.Errorf("Context = %q, want it to start with the surrounding text", ctx)
	}
	if n := len([]rune(ctx)); n < 250 || n > saveContextMaxRunes {
		t.Errorf("len(Context) = %d runes, want it truncated to <= %d", n, saveContextMaxRunes)
	}
}

// saveThreadPageHTML is a minimal XenForo page wiring save candidates into the
// first post, to prove parseThreadHTML populates ThreadData end-to-end.
const saveThreadPageHTML = `<!DOCTYPE html>
<html><body>
<div class="p-body">
  <div class="p-title"><h1 class="p-title-value">Save Test Game [v1.0]</h1></div>
  <div class="messageList">
    <article class="message message--post">
      <div class="message-content">
        <div class="bbWrapper">
          <h2>Overview</h2>
          <p>Version: 1.0</p>
          <p>Developer: TestDev</p>
          <h3>Download</h3>
          <ul>
            <li><a href="https://mega.nz/file/game">MEGA</a></li>
          </ul>
          <h3>Extras</h3>
          <ul>
            <li><a href="https://f95zone.to/attachments/Save-Test-Game-Full-Save.482910/">full save</a></li>
            <li><a href="https://gofile.io/d/SaveFiles">gallery saves</a></li>
          </ul>
        </div>
      </div>
    </article>
  </div>
</div>
</body></html>`

func TestParseThreadHTML_PopulatesSaveCandidates(t *testing.T) {
	t.Parallel()

	td, err := parseThreadHTML(saveThreadPageHTML, "https://f95zone.to/threads/save-test.7777/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	if len(td.SaveCandidates) != 2 {
		t.Fatalf("got %d save candidates, want 2: %+v", len(td.SaveCandidates), td.SaveCandidates)
	}

	att := td.SaveCandidates[0]
	if att.Kind != SaveCandidateAttachment || att.Host != "f95zone" {
		t.Errorf("candidate 0 = %q/%q, want attachment/f95zone", att.Kind, att.Host)
	}
	if att.Filename != "Save-Test-Game-Full-Save" {
		t.Errorf("candidate 0 Filename = %q", att.Filename)
	}

	ext := td.SaveCandidates[1]
	if ext.Kind != SaveCandidateExternal || ext.Host != "gofile" {
		t.Errorf("candidate 1 = %q/%q, want external/gofile", ext.Kind, ext.Host)
	}

	// Attachment links must not leak into the regular download links.
	for _, dl := range td.DownloadLinks {
		if strings.Contains(dl.URL, "/attachments/") {
			t.Errorf("attachment %q leaked into DownloadLinks", dl.URL)
		}
	}
	if len(td.DownloadLinks) != 2 {
		t.Errorf("DownloadLinks = %+v, want the two game download hosts", td.DownloadLinks)
	}
}
