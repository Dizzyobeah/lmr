package gemini

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"
)

func TestNewClientHardening(t *testing.T) {
	c := NewClient("https://x/v1", "k", time.Second)
	tr := c.http.Transport.(*http.Transport)
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("TLS MinVersion = %+v, want TLS 1.2", tr.TLSClientConfig)
	}
	if c.http.CheckRedirect == nil || c.http.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("CheckRedirect must return http.ErrUseLastResponse")
	}
}
