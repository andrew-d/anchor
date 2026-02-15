package anchor

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestWebUI_Dashboard(t *testing.T) {
	app := testApp(t)
	addr := app.HTTPAddrForTest()

	resp, err := http.Get(fmt.Sprintf("http://%s/", addr))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected text/html content type, got %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)

	// Should contain the node ID.
	if !strings.Contains(html, "test-node") {
		t.Error("dashboard does not contain node ID")
	}

	// Should show Leader state.
	if !strings.Contains(html, "Leader") {
		t.Error("dashboard does not contain Leader state")
	}

	// Should contain the deployment ID.
	if !strings.Contains(html, app.DeploymentID()) {
		t.Error("dashboard does not contain deployment ID")
	}
}

func TestWebUI_Docs(t *testing.T) {
	app := testApp(t)
	addr := app.HTTPAddrForTest()

	resp, err := http.Get(fmt.Sprintf("http://%s/docs", addr))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected text/html content type, got %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)

	for _, ep := range apiEndpoints {
		if !strings.Contains(html, ep.Pattern) {
			t.Errorf("docs page missing endpoint pattern %q", ep.Pattern)
		}
	}
}

func TestWebUI_StaticCSS(t *testing.T) {
	app := testApp(t)
	addr := app.HTTPAddrForTest()

	resp, err := http.Get(fmt.Sprintf("http://%s/static/style.css", addr))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
