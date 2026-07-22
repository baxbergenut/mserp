package repository

import "testing"

func TestPersonNameTokenSignatureIgnoresOrderAndPunctuation(t *testing.T) {
	left := personNameTokenSignature("Jane Mary Doe")
	right := personNameTokenSignature("Doe, Jane Mary")
	if left != right {
		t.Fatalf("signatures differ: %q != %q", left, right)
	}
}

func TestExcludedRelayFees(t *testing.T) {
	for _, feeType := range []string{"sender_fee", "Platform_Fee", " deposit "} {
		if !isExcludedRelayFee(feeType) {
			t.Fatalf("expected %q to be excluded", feeType)
		}
	}
	if isExcludedRelayFee("merchant_service_fee") {
		t.Fatal("merchant service fee should remain reportable")
	}
}
