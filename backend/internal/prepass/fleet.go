package prepass

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Fleet v2 is for bypass/tolling tractors, not VPP records or trailer plates.
type Vehicle struct {
	AccountNumber       string `json:"accountNumber"`
	CostCenter          string `json:"costCenter"`
	VIN                 string `json:"vin"`
	VehicleNumber       string `json:"vehicleNumber"`
	TempPlate           bool   `json:"tempPlate"`
	LicensePlateNumber  string `json:"licensePlateNumber"`
	LicensePlateState   string `json:"licensePlateState"`
	DeclaredWeight      int    `json:"declaredWeight"`
	IRPNumber           string `json:"irpNumber"`
	IRPState            string `json:"irpState"`
	IFTANumber          string `json:"iftaNumber"`
	IFTAState           string `json:"iftaState"`
	TransponderNumber   string `json:"transponderNumber"`
	VRN                 string `json:"vrn"`
	HazmatHauler        bool   `json:"hazmatHauler"`
	VehicleImage        string `json:"vehicleImage"`
	LeasedVehicle       bool   `json:"leasedVehicle"`
	LeasingCompany      string `json:"leasingCompany"`
	LeaseExpirationDate string `json:"leaseExpirationDate"`
}
type Transponder struct {
	AccountNumber     string `json:"accountNumber"`
	CostCenter        string `json:"costCenter"`
	VIN               string `json:"vin"`
	VehicleNumber     string `json:"vehicleNumber"`
	TransponderNumber string `json:"transponderNumber"`
	VRN               string `json:"vrn"`
}
type FleetSnapshot struct {
	Accounts     []string      `json:"accounts"`
	Vehicles     []Vehicle     `json:"vehicles"`
	Transponders []Transponder `json:"transponders"`
}
type Plate struct {
	Temporary bool   `json:"temporary"`
	Number    string `json:"number"`
	State     string `json:"state"`
}
type Registration struct {
	AccountNumber string `json:"accountNumber"`
	State         string `json:"state"`
}

// These editable fields deliberately omit assignment, color and lease details.
// PATCH leaves the remaining provider fields intact; device assignment is its own action.
type VehicleInput struct {
	VehicleNumber  string       `json:"vehicleNumber"`
	LicensePlate   Plate        `json:"licensePlate"`
	DeclaredWeight int          `json:"declaredWeight"`
	IRP            Registration `json:"irp"`
	IFTA           Registration `json:"ifta"`
	VRN            string       `json:"vrn"`
	HazmatHauler   bool         `json:"hazmatHauler"`
}

func (v Vehicle) Input() VehicleInput {
	return VehicleInput{v.VehicleNumber, Plate{v.TempPlate, v.LicensePlateNumber, v.LicensePlateState}, v.DeclaredWeight, Registration{v.IRPNumber, v.IRPState}, Registration{v.IFTANumber, v.IFTAState}, v.VRN, v.HazmatHauler}
}

var FleetVIN = regexp.MustCompile(`^[A-Z0-9]{16,19}$`)
var TransponderNumber = regexp.MustCompile(`^[0-9]{9,12}$`)
var unitNumber = regexp.MustCompile(`^[A-Za-z0-9-]{1,8}$`)
var plateNumber = regexp.MustCompile(`^[A-Z0-9-]{5,9}$`)
var irpNumber = regexp.MustCompile(`^[A-Za-z0-9]{1,10}$`)
var iftaNumber = regexp.MustCompile(`^[0-9]{1,16}[A-Za-z]{0,2}$`)

const stateCodes = " AL AK AZ AR CA CO CT DE FL GA HI ID IL IN IA KS KY LA ME MD MA MI MN MS MO MT NE NV NH NJ NM NY NC ND OH OK OR PA RI SC SD TN TX UT VT VA WA WV WI WY AB BC MB NB NL NS NT ON PE QC SK YT AG BS CH CI CL CM CU DF DU GE GJ HD JA MC MR MX NA NU OA PU QE QI SI SL SO TB TL TM VE YU BN TA "

func ValidState(s string) bool { return len(s) == 2 && strings.Contains(stateCodes, " "+s+" ") }
func (p *Plate) Validate() error {
	p.Number = strings.ToUpper(strings.TrimSpace(p.Number))
	p.State = strings.ToUpper(strings.TrimSpace(p.State))
	if !ValidState(p.State) || (!p.Temporary || p.Number != "") && !plateNumber.MatchString(p.Number) {
		return fmt.Errorf("Provide a valid plate state and a 5–9 character plate number (optional for temporary plates)")
	}
	return nil
}
func (v *VehicleInput) Validate() error {
	v.VehicleNumber = strings.TrimSpace(v.VehicleNumber)
	v.LicensePlate.Number = strings.ToUpper(strings.TrimSpace(v.LicensePlate.Number))
	v.LicensePlate.State = strings.ToUpper(strings.TrimSpace(v.LicensePlate.State))
	v.IRP.AccountNumber = strings.TrimSpace(v.IRP.AccountNumber)
	v.IRP.State = strings.ToUpper(strings.TrimSpace(v.IRP.State))
	v.IFTA.AccountNumber = strings.TrimSpace(v.IFTA.AccountNumber)
	v.IFTA.State = strings.ToUpper(strings.TrimSpace(v.IFTA.State))
	v.VRN = strings.TrimSpace(v.VRN)
	if !unitNumber.MatchString(v.VehicleNumber) {
		return fmt.Errorf("PrePass unit numbers must contain 1–8 letters, digits or hyphens")
	}
	if !ValidState(v.LicensePlate.State) || (!v.LicensePlate.Temporary || v.LicensePlate.Number != "") && !plateNumber.MatchString(v.LicensePlate.Number) {
		return fmt.Errorf("Provide a valid plate state and a 5–9 character plate number (optional for temporary plates)")
	}
	if v.DeclaredWeight < 0 || v.DeclaredWeight > 600000 {
		return fmt.Errorf("Declared weight must be between 0 and 600,000 pounds")
	}
	if !irpNumber.MatchString(v.IRP.AccountNumber) || !ValidState(v.IRP.State) {
		return fmt.Errorf("Provide the IRP account number and state")
	}
	if len(v.IFTA.AccountNumber) > 10 || !iftaNumber.MatchString(v.IFTA.AccountNumber) || !ValidState(v.IFTA.State) {
		return fmt.Errorf("Provide the IFTA account number (up to 10 characters) and state")
	}
	if len(v.VRN) > 20 {
		return fmt.Errorf("Vehicle reference number is too long")
	}
	return nil
}

type FleetAPIError struct {
	Status    int
	Uncertain bool
}

func (e *FleetAPIError) Error() string {
	if e.Uncertain {
		return "PrePass did not confirm the request. Review its result before making another change."
	}
	return fmt.Sprintf("PrePass rejected the request (HTTP %d). Review the vehicle details and account access.", e.Status)
}

func (c *Client) Fleet(ctx context.Context) (FleetSnapshot, error) {
	token, accounts, err := c.credentials(ctx)
	out := FleetSnapshot{Accounts: accounts, Vehicles: []Vehicle{}, Transponders: []Transponder{}}
	if err != nil {
		return out, err
	}
	for _, account := range accounts {
		q := "?" + url.Values{"accountNumber": {account}}.Encode()
		var vehicles []Vehicle
		var devices []Transponder
		if err = c.fleetRequest(ctx, http.MethodGet, "/vehicles"+q, token, nil, &vehicles); err != nil {
			return out, err
		}
		if err = c.fleetRequest(ctx, http.MethodGet, "/transponders"+q, token, nil, &devices); err != nil {
			return out, err
		}
		for _, v := range vehicles {
			if v.AccountNumber != account || strings.TrimSpace(v.VIN) == "" {
				return out, fmt.Errorf("PrePass returned an invalid vehicle roster")
			}
		}
		for _, v := range devices {
			if v.AccountNumber != account || v.TransponderNumber == "" {
				return out, fmt.Errorf("PrePass returned an invalid transponder roster")
			}
		}
		out.Vehicles = append(out.Vehicles, vehicles...)
		out.Transponders = append(out.Transponders, devices...)
	}
	return out, nil
}
func (c *Client) ChangeVehicle(ctx context.Context, action, account, vin string, input VehicleInput, device string) error {
	token, accounts, err := c.credentials(ctx)
	if err != nil {
		return err
	}
	allowed := false
	for _, a := range accounts {
		if a == account {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("Select an authorized PrePass account")
	}
	q := url.Values{"accountNumber": {account}}
	method := http.MethodPatch
	var body any = input
	switch action {
	case "add":
		method = http.MethodPost
		body = struct {
			VIN string `json:"vin"`
			VehicleInput
		}{vin, input}
	case "edit":
		q.Set("vin", vin)
	case "plate":
		q.Set("vin", vin)
		body = map[string]Plate{"licensePlate": input.LicensePlate}
	case "remove":
		method = http.MethodDelete
		q.Set("vin", vin)
		body = nil
	case "assign":
		q.Set("vin", vin)
		body = map[string]string{"transponderNumber": device}
	default:
		return fmt.Errorf("Unsupported PrePass action")
	}
	return c.fleetRequest(ctx, method, "/vehicle?"+q.Encode(), token, body, nil)
}
func (c *Client) fleetRequest(ctx context.Context, method, path, token string, body, target any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/fleetmanagement/v2"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return &FleetAPIError{Uncertain: method != http.MethodGet}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		// Never echo vendor response bodies, which can contain customer data.
		definitive := res.StatusCode == 400 || res.StatusCode == 401 || res.StatusCode == 403 || res.StatusCode == 404 || res.StatusCode == 422
		return &FleetAPIError{Status: res.StatusCode, Uncertain: method != http.MethodGet && !definitive}
	}
	if target == nil {
		return nil
	}
	// Fleet lists document JSON arrays, including [] for no records. An empty
	// HTTP response must not erase a previously complete roster.
	if res.StatusCode == http.StatusNoContent {
		return fmt.Errorf("PrePass returned an empty roster response")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16*1024*1024+1))
	if err != nil || len(raw) > 16*1024*1024 {
		return fmt.Errorf("PrePass roster response could not be read")
	}
	if err = json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("PrePass returned an invalid roster response")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("PrePass returned an empty roster response")
	}
	return nil
}
