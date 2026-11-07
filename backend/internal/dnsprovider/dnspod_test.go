package dnsprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A hostname without records makes DNSPod answer with an error code. That must
// be treated as an empty list, otherwise the very first push can never create
// the record (regression: "自动下发失败: 读取解析记录失败: DNSPod: 记录列表为空").
func TestDNSPodListAEmptyIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Response":{"Error":{"Code":"ResourceNotFound.NoDataOfRecord","Message":"记录列表为空。"},"RequestId":"x"}}`))
	}))
	defer srv.Close()

	p := &DNSPod{secretID: "id", secretKey: "key", client: srv.Client(), endpoint: srv.URL}
	records, err := p.ListA(context.Background(), "example.com", "cdn", "默认")
	if err != nil {
		t.Fatalf("empty record list must not fail, got: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected no records, got %d", len(records))
	}
}

func TestDNSPodListAParsesRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Response":{"RecordCountInfo":{"TotalCount":1},"RecordList":[{"RecordId":123,"Value":"1.2.3.4","Name":"cdn","Line":"默认","TTL":600,"Status":"ENABLE"}],"RequestId":"x"}}`))
	}))
	defer srv.Close()

	p := &DNSPod{secretID: "id", secretKey: "key", client: srv.Client(), endpoint: srv.URL}
	records, err := p.ListA(context.Background(), "example.com", "cdn", "默认")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Value != "1.2.3.4" || records[0].RecordID != "123" {
		t.Fatalf("unexpected records: %+v", records)
	}
}

// Other failures must still surface.
func TestDNSPodListAKeepsRealErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Response":{"Error":{"Code":"AuthFailure.SignatureFailure","Message":"签名错误"},"RequestId":"x"}}`))
	}))
	defer srv.Close()

	p := &DNSPod{secretID: "id", secretKey: "key", client: srv.Client(), endpoint: srv.URL}
	if _, err := p.ListA(context.Background(), "example.com", "cdn", "默认"); err == nil {
		t.Fatal("signature failure must not be swallowed")
	}
}

func TestIsNotFound(t *testing.T) {
	notFound := &apiError{Provider: "x", Status: http.StatusNotFound}
	if !isNotFound(notFound) {
		t.Fatal("404 should be recognised as not found")
	}
	if isNotFound(&apiError{Provider: "x", Status: http.StatusForbidden}) {
		t.Fatal("403 must not be treated as not found")
	}
}
