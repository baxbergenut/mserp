package groq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractCabCardSendsVisionRequestAndNormalizesFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["model"] != "vision-model" {
			t.Errorf("model = %v", request["model"])
		}
		encoded, _ := json.Marshal(request)
		if !strings.Contains(string(encoded), "data:image/png;base64,iVBORw==") {
			t.Errorf("request does not contain image data: %s", encoded)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
          "choices": [{"message": {"content": "{\"unitNumber\":\" 1042 \",\"vin\":\"1fujgldr0dlbs1234\",\"year\":2013,\"make\":\"Freightliner\",\"model\":\"Cascadia\",\"licensePlate\":\" ab 123 \" ,\"licenseState\":\"il\",\"registrationExpires\":\"2027-01-31\"}"}}]
        }`))
	}))
	defer server.Close()

	client := NewClient("secret", "vision-model")
	client.baseURL = server.URL
	client.httpClient = server.Client()

	fields, err := client.ExtractCabCard(context.Background(), []Image{{
		ContentType: "image/png",
		Data:        []byte{0x89, 'P', 'N', 'G'},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if fields.UnitNumber != "1042" {
		t.Errorf("UnitNumber = %q", fields.UnitNumber)
	}
	if fields.VIN != "1FUJGLDR0DLBS1234" {
		t.Errorf("VIN = %q", fields.VIN)
	}
	if fields.LicensePlate != "AB123" || fields.LicenseState != "IL" {
		t.Errorf("plate = %q %q", fields.LicensePlate, fields.LicenseState)
	}
	if fields.RegistrationExpires != "2027-01-31" {
		t.Errorf("registration expiration = %q", fields.RegistrationExpires)
	}
}

func TestExtractCabCardRequiresConfiguration(t *testing.T) {
	client := NewClient("", "vision-model")
	_, err := client.ExtractCabCard(context.Background(), []Image{{
		ContentType: "image/png",
		Data:        []byte("image"),
	}})
	if err != ErrNotConfigured {
		t.Fatalf("error = %v, want ErrNotConfigured", err)
	}
}

func TestNormalizeCabCardFieldsDropsUnsafeValues(t *testing.T) {
	year := 1800
	fields := normalizeCabCardFields(CabCardFields{
		VIN:                 "partial-vin",
		Year:                &year,
		LicenseState:        "Illinois",
		RegistrationExpires: "January 2027",
	})
	if fields.VIN != "" || fields.Year != nil || fields.LicenseState != "" || fields.RegistrationExpires != "" {
		t.Fatalf("invalid extracted values were retained: %+v", fields)
	}
}

func TestNormalizeCDLFields(t *testing.T) {
	fields := normalizeCDLFields(CDLFields{
		FullName:       "  JANE   Q DRIVER ",
		LicenseNumber:  " d123 4567 ",
		LicenseState:   "il",
		LicenseExpires: "2028-04-30",
		Address:        "  123   Main St ",
		City:           "  Elk   Grove ",
		State:          "il",
		PostalCode:     " 60007 ",
	})
	if fields.FullName != "JANE Q DRIVER" || fields.LicenseNumber != "D123 4567" {
		t.Fatalf("identity fields not normalized: %+v", fields)
	}
	if fields.LicenseState != "IL" || fields.State != "IL" || fields.LicenseExpires != "2028-04-30" {
		t.Fatalf("license fields not normalized: %+v", fields)
	}
	if fields.Address != "123 Main St" || fields.City != "Elk Grove" || fields.PostalCode != "60007" {
		t.Fatalf("address fields not normalized: %+v", fields)
	}
}
