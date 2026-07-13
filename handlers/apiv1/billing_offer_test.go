package apiv1

import "testing"

func TestNormalizeCheckoutOffer(t *testing.T) {
	tests := []struct {
		name    string
		offer   string
		plan    string
		want    string
		wantErr bool
	}{
		{name: "empty", offer: "", plan: "pro", want: ""},
		{name: "founding pro", offer: "founding", plan: "pro", want: "founding"},
		{name: "founding case", offer: " Founding ", plan: "pro", want: "founding"},
		{name: "founding max", offer: "founding", plan: "max", wantErr: true},
		{name: "unknown", offer: "launch", plan: "pro", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeCheckoutOffer(test.offer, test.plan)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeCheckoutOffer() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("normalizeCheckoutOffer() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeSiteOffer(t *testing.T) {
	if got := normalizeSiteOffer(" Founding "); got != "founding" {
		t.Fatalf("normalizeSiteOffer() = %q, want founding", got)
	}
	if got := normalizeSiteOffer("unknown"); got != "" {
		t.Fatalf("normalizeSiteOffer() = %q, want empty", got)
	}
}
