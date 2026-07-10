package apiv1

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestStaticDocsServeAIHedgeFundAdapter(t *testing.T) {
	app := fiber.New()
	(&handlers{}).mountStaticDocs(app)

	request := httptest.NewRequest(http.MethodGet, "/api/ai_hedge_fund_adapter.py", nil)
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("GET adapter: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/x-python") {
		t.Fatalf("content type = %q, want Python", contentType)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read adapter: %v", err)
	}
	if !strings.Contains(string(body), "class AsOfStrategy") {
		t.Fatal("adapter response is missing its as-of strategy")
	}
}
