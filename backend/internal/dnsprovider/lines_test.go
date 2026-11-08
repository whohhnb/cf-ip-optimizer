package dnsprovider

import "testing"

// Carrier-level lines are a vendor capability, not a universal feature. Only
// vendors whose API actually accepts a line parameter may advertise them —
// writing 电信 to a vendor that ignores it silently creates a second record on
// the default line, which is worse than refusing.
func TestOnlyCarrierAwareVendorsDeclareLines(t *testing.T) {
	withLines := map[string]bool{}
	for _, d := range List() {
		desc, ok := DescriptorOf(d.Name)
		if !ok {
			t.Fatalf("descriptor missing for %s", d.Name)
		}
		if len(desc.Lines) > 0 {
			withLines[d.Name] = true
		}
	}
	for _, want := range []string{"dnspod", "aliyun"} {
		if !withLines[want] {
			t.Fatalf("%s must declare its carrier lines", want)
		}
	}
	for _, mustNot := range []string{"cloudflare", "godaddy", "huawei", "porkbun"} {
		if withLines[mustNot] {
			t.Fatalf("%s has no line concept but declares lines", mustNot)
		}
	}
}

func TestEveryVendorHasExactlyOneDefaultLine(t *testing.T) {
	for _, name := range []string{"dnspod", "aliyun"} {
		desc, _ := DescriptorOf(name)
		n := 0
		for _, l := range desc.Lines {
			if l.Label == "默认" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s must have exactly one 默认 line, got %d", name, n)
		}
	}
}

func TestSupportsLine(t *testing.T) {
	dnspod, _ := DescriptorOf("dnspod")
	if !dnspod.SupportsLine("电信") {
		t.Fatal("dnspod must accept 电信")
	}
	if dnspod.SupportsLine("telecom") {
		t.Fatal("dnspod uses Chinese line names, telecom must be rejected")
	}
	if !dnspod.SupportsLine("") {
		t.Fatal("empty line means default and must be allowed")
	}

	aliyun, _ := DescriptorOf("aliyun")
	if !aliyun.SupportsLine("telecom") {
		t.Fatal("aliyun must accept telecom")
	}
	if aliyun.SupportsLine("电信") {
		t.Fatal("aliyun uses english codes, 电信 must be rejected")
	}
}

func TestAliyunLineDefaults(t *testing.T) {
	if got := aliyunLine(""); got != "default" {
		t.Fatalf("empty line must map to default, got %q", got)
	}
	if got := aliyunLine("telecom"); got != "telecom" {
		t.Fatalf("explicit line must pass through, got %q", got)
	}
}
