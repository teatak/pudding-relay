package httpserver

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAdminPageIsPrivateSelfContainedAndBilingual(t *testing.T) {
	_, server, token := testRelay(t)
	response, err := http.Get(server.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	if response.StatusCode != 200 {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("admin page must not be cached")
	}
	for _, directive := range []string{"default-src 'none'", "connect-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(response.Header.Get("Content-Security-Policy"), directive) {
			t.Fatal("missing admin CSP")
		}
	}
	for _, control := range []string{"id=\"login\"", "id=\"desktops\"", "id=\"credential\"", "id=\"copyToken\"", "id=\"revokeDialog\"", "id=\"disconnect\""} {
		if !strings.Contains(page, control) {
			t.Fatalf("missing control %s", control)
		}
	}
	for _, text := range []string{"Registered desktops", "已登记的电脑", "Copy and save it now", "请立即复制保存"} {
		if !strings.Contains(page, text) {
			t.Fatalf("missing translation %s", text)
		}
	}
	for _, unsafe := range []string{token, strings.Repeat("a", 32), "localStorage", "sessionStorage", "<script src="} {
		if strings.Contains(page, unsafe) {
			t.Fatal("admin page leaks authentication or loads an external script")
		}
	}
}
