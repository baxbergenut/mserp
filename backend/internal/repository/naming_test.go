package repository

import (
	"testing"

	"mserp/internal/relay"
)

func TestPersonNameTokenSignatureIgnoresOrderAndPunctuation(t *testing.T) {
	left := personNameTokenSignature("Jane Mary Doe")
	right := personNameTokenSignature("Doe, Jane Mary")
	if left != right {
		t.Fatalf("signatures differ: %q != %q", left, right)
	}
}

func TestRelayDriverNameMatchQuality(t *testing.T) {
	tests := []struct {
		name  string
		relay string
		local string
		min   int
	}{
		{name: "middle name omitted", relay: "Allen Japhet Nshimiye", local: "Allen Nshimiye", min: 90},
		{name: "reordered with suffix", relay: "Murray Errick Terell Sr", local: "Errick Terell Murray", min: 100},
		{name: "punctuation and initials", relay: "Donzo Ayouba,F", local: "Ayouba Donzo", min: 100},
		{name: "small upstream typo", relay: "Faradji Yuyisenge", local: "Faradji Tuyisenge", min: 80},
		{name: "diacritics", relay: "José Núñez", local: "Jose Nunez", min: 100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if quality := relayDriverNameMatchQuality(test.relay, test.local); quality < test.min {
				t.Fatalf("quality = %d, want at least %d", quality, test.min)
			}
		})
	}

	if quality := relayDriverNameMatchQuality("Justin Munyantore", "Justin Mpumuye"); quality >= 80 {
		t.Fatalf("unrelated surnames received automatic-match quality %d", quality)
	}
}

func TestShouldPreferMoreCompletePersonName(t *testing.T) {
	if !shouldPreferMoreCompletePersonName("James Lee Burligh", "Burligh James") {
		t.Fatal("expected the DataTruck name with a middle name to be preferred")
	}
	if shouldPreferMoreCompletePersonName("Burligh James", "James Lee Burligh") {
		t.Fatal("a shorter source name must not replace the canonical name")
	}
	if shouldPreferMoreCompletePersonName("Luis Figueredo", "Hector Torres Robles") {
		t.Fatal("an unrelated source name must not replace the canonical name")
	}
}

func TestChooseUniqueCompatibleDriver(t *testing.T) {
	id, name, found, err := chooseUniqueCompatibleDriver([]driverNameCandidate{{
		id:       "driver-1",
		fullName: "James Lee Burligh",
	}})
	if err != nil || !found || id != "driver-1" || name != "James Lee Burligh" {
		t.Fatalf("match = %q, %q, %v, %v", id, name, found, err)
	}
	if _, _, found, err := chooseUniqueCompatibleDriver([]driverNameCandidate{
		{id: "driver-1", fullName: "James Lee Burligh"},
		{id: "driver-2", fullName: "James Burligh"},
	}); err != nil || found {
		t.Fatalf("ambiguous compatible names must not auto-match: found=%v err=%v", found, err)
	}
}

func TestChooseRelayDriverCandidateUsesContactAndFleetEvidence(t *testing.T) {
	email := "driver@example.com"
	phone := "(616) 306-3564"
	driver := relay.TransactionDriver{
		FirstName: "Allen Japhet",
		LastName:  "Nshimiye",
		Phone:     "+16163063564",
		Email:     &email,
	}
	candidates := []relayDriverCandidate{
		{
			id:       "relay-created",
			fullName: "Allen Japhet Nshimiye",
			phone:    pointerTo("+1 616-306-3564"),
			email:    &email,
		},
		{
			id:               "fleet-driver",
			fullName:         "Allen Nshimiye",
			phone:            &phone,
			email:            &email,
			hasFleetEvidence: true,
		},
	}

	id, found, err := chooseRelayDriverCandidate(driver, "Allen Japhet Nshimiye", candidates)
	if err != nil {
		t.Fatal(err)
	}
	if !found || id != "fleet-driver" {
		t.Fatalf("match = %q, %v; want fleet-driver", id, found)
	}
}

func TestChooseRelayDriverCandidateRejectsUnrelatedSharedContact(t *testing.T) {
	email := "dispatch@example.com"
	driver := relay.TransactionDriver{
		FirstName: "Luis",
		LastName:  "Figueredo",
		Email:     &email,
	}
	candidates := []relayDriverCandidate{{
		id:               "different-driver",
		fullName:         "Hector Torres Robles",
		email:            &email,
		hasFleetEvidence: true,
	}}

	if id, found, err := chooseRelayDriverCandidate(driver, "Luis Figueredo", candidates); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatalf("unexpected match %q", id)
	}
}

func pointerTo(value string) *string {
	return &value
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
