package downloader

import "testing"

func TestScoreLinkHost_BrowserGatedHosts(t *testing.T) {
	prev := BrowserResolverEnabled()
	defer SetBrowserResolver(prev)

	SetBrowserResolver(false)
	if got := ScoreLinkHost("datanodes"); got != -50 {
		t.Errorf("datanodes without browser = %d, want -50", got)
	}
	if got := ScoreLinkHost("vikingfile"); got != -50 {
		t.Errorf("vikingfile without browser = %d, want -50", got)
	}
	if got := ScoreLinkHost("krakenfiles"); got != -50 {
		t.Errorf("krakenfiles without browser = %d, want -50", got)
	}

	SetBrowserResolver(true)
	if got := ScoreLinkHost("datanodes"); got != 10 {
		t.Errorf("datanodes with browser = %d, want 10", got)
	}
	if got := ScoreLinkHost("vikingfile"); got != 10 {
		t.Errorf("vikingfile with browser = %d, want 10", got)
	}
	if got := ScoreLinkHost("krakenfiles"); got != 10 {
		t.Errorf("krakenfiles with browser = %d, want 10", got)
	}
}
