package fleetscope

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

//go:embed protocol.json
var Protocol []byte

//go:embed wire.schema.json
var WireSchema []byte

const OwnershipCapability = "mserp-fleet-ownership-v1"
const SyncBodyLimit = 4 << 20

func ContractHash() string { h := sha256.Sum256(Protocol); return hex.EncodeToString(h[:]) }
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func ValidID(s string) bool {
	return uuidPattern.MatchString(s) && s != "00000000-0000-0000-0000-000000000000"
}

type SyncOptions struct {
	Mode            string
	URL             string
	CompanyID       string
	Environment     string
	KeyID           string
	Secret          string
	BootstrapKeyID  string
	BootstrapSecret string
}

func (o SyncOptions) Configured() bool { return o.Mode != "" && o.Mode != "off" }
func (o SyncOptions) Validate() error {
	if o.Mode == "" {
		o.Mode = "off"
	}
	if o.Mode != "off" && o.Mode != "reconcile" && o.Mode != "live" {
		return errors.New("FLEETSCOPE_SYNC_MODE must be off, reconcile or live")
	}
	if o.URL == "" && o.CompanyID == "" && o.KeyID == "" && o.Secret == "" && o.BootstrapKeyID == "" && o.BootstrapSecret == "" && o.Environment == "" && o.Mode == "off" {
		return nil
	}
	u, e := url.Parse(o.URL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || strings.HasSuffix(o.URL, "/") || !strings.HasSuffix(u.Path, "/integrations/mserp/v1") {
		return errors.New("FLEETSCOPE_SYNC_URL must be a fixed HTTPS URL ending in /integrations/mserp/v1 without a trailing slash")
	}
	if !ValidID(o.CompanyID) || (o.Environment != "production" && o.Environment != "staging") || !validToken(o.KeyID) || len(o.Secret) < 32 {
		return errors.New("FleetScope sync requires company UUID, production/staging environment, key ID and a secret of at least 32 bytes")
	}
	if (o.BootstrapKeyID != "" || o.BootstrapSecret != "") && (!validToken(o.BootstrapKeyID) || len(o.BootstrapSecret) < 32 || o.BootstrapKeyID == o.KeyID || o.BootstrapSecret == o.Secret) {
		return errors.New("FleetScope bootstrap requires a distinct key ID and secret of at least 32 bytes")
	}
	return nil
}
func validToken(s string) bool {
	if len(s) < 1 || len(s) > 80 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

type Ownership struct {
	Kind               string  `json:"kind"`
	MSERPOwnerID       string  `json:"mserpOwnerId"`
	DisplayName        string  `json:"displayName"`
	MSERPOwnerDriverID *string `json:"mserpOwnerDriverId"`
	EffectiveDate      *string `json:"effectiveDate"`
}

func (o Ownership) Validate() error {
	if !ValidID(o.MSERPOwnerID) || strings.TrimSpace(o.DisplayName) != o.DisplayName || len([]rune(o.DisplayName)) == 0 || len([]rune(o.DisplayName)) > 200 {
		return errors.New("invalid owner identity")
	}
	for _, r := range o.DisplayName {
		if unicode.IsControl(r) {
			return errors.New("invalid owner name")
		}
	}
	if o.Kind != "company" && o.Kind != "driver" && o.Kind != "investor" {
		return errors.New("invalid ownership kind")
	}
	if (o.Kind == "driver") != (o.MSERPOwnerDriverID != nil) {
		return errors.New("owner driver identity does not match kind")
	}
	if o.MSERPOwnerDriverID != nil && !ValidID(*o.MSERPOwnerDriverID) {
		return errors.New("invalid owner driver identity")
	}
	if o.EffectiveDate != nil {
		if _, e := time.Parse(time.DateOnly, *o.EffectiveDate); e != nil {
			return errors.New("invalid ownership date")
		}
	}
	return nil
}

type TruckProjection struct {
	MSERPTruckID      string    `json:"mserpTruckId"`
	UnitNumber        string    `json:"unitNumber"`
	VIN               *string   `json:"vin"`
	Year              *int      `json:"year"`
	Make              *string   `json:"make"`
	Model             *string   `json:"model"`
	LicensePlate      *string   `json:"licensePlate"`
	LicenseState      *string   `json:"licenseState"`
	OperationalActive bool      `json:"operationalActive"`
	OperationalStatus string    `json:"operationalStatus"`
	Ownership         Ownership `json:"ownership"`
}
type Provisioning struct {
	Color      string `json:"color"`
	TruckClass string `json:"truckClass"`
	GVWRLbs    int    `json:"gvwrLbs"`
}
type DriverRef struct {
	MSERPDriverID      string `json:"mserpDriverId"`
	FleetScopeDriverID string `json:"fleetScopeDriverId"`
}
type Crew struct {
	MSERPTruckID        string     `json:"mserpTruckId"`
	FleetScopeVehicleID string     `json:"fleetScopeVehicleId"`
	ExpectedRevision    string     `json:"expectedRevision"`
	Primary             *DriverRef `json:"primary"`
	Secondary           *DriverRef `json:"secondary"`
}
type Operation struct {
	Version            int              `json:"version"`
	OperationID        string           `json:"operationId"`
	CompanyID          string           `json:"companyId"`
	Environment        string           `json:"environment"`
	Epoch              string           `json:"epoch"`
	OccurredAt         time.Time        `json:"occurredAt"`
	Kind               string           `json:"kind"`
	SourceRevision     int64            `json:"sourceRevision"`
	AssignmentSequence int64            `json:"assignmentSequence"`
	SnapshotID         string           `json:"snapshotId,omitempty"`
	ExpectedRevision   string           `json:"expectedRevision,omitempty"`
	FleetScopeID       string           `json:"fleetScopeId,omitempty"`
	MSERPID            string           `json:"mserpId,omitempty"`
	HireEventID        string           `json:"hireEventId,omitempty"`
	Truck              *TruckProjection `json:"truck,omitempty"`
	Provisioning       *Provisioning    `json:"provisioning,omitempty"`
	Crews              []Crew           `json:"crews,omitempty"`
}

func (o Operation) Path() string {
	switch o.Kind {
	case "driver.link":
		return "/driver-links"
	case "truck.link":
		return "/truck-links"
	case "truck.upsert":
		return "/trucks/upsert"
	case "truck.retire":
		return "/trucks/retire"
	case "assignments.apply":
		return "/assignments/apply"
	}
	return ""
}

type Result struct {
	OperationID        string `json:"operationId"`
	Status             string `json:"status"`
	CompanyID          string `json:"companyId"`
	Environment        string `json:"environment"`
	Epoch              string `json:"epoch"`
	FleetScopeID       string `json:"fleetScopeId"`
	Revision           string `json:"revision"`
	AssignmentRevision string `json:"assignmentRevision"`
	SourceRevision     int64  `json:"sourceRevision"`
	AssignmentSequence int64  `json:"assignmentSequence"`
	Crews              []Crew `json:"crews"`
}
type Capabilities struct {
	Version      int    `json:"version"`
	ContractHash string `json:"contractHash"`
	Ownership    string `json:"ownership"`
	CompanyID    string `json:"companyId"`
	Environment  string `json:"environment"`
	Epoch        string `json:"epoch"`
	Mode         string `json:"mode"`
}
type RemoteDriver struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Revision string `json:"revision"`
	MSERPID  string `json:"mserpId"`
}
type RemoteTruck struct {
	ID                 string           `json:"id"`
	UnitNumber         string           `json:"unitNumber"`
	VIN                string           `json:"vin"`
	Revision           string           `json:"revision"`
	AssignmentRevision string           `json:"assignmentRevision"`
	MSERPID            string           `json:"mserpId"`
	PrimaryID          string           `json:"primaryId"`
	SecondaryID        string           `json:"secondaryId"`
	SafetyHold         bool             `json:"safetyHold"`
	Ownership          json.RawMessage  `json:"ownership"`
	Projection         *TruckProjection `json:"projection"`
}
type Snapshot struct {
	ID          string         `json:"id"`
	CompanyID   string         `json:"companyId"`
	Environment string         `json:"environment"`
	Epoch       string         `json:"epoch"`
	CapturedAt  time.Time      `json:"capturedAt"`
	NextCursor  string         `json:"nextCursor"`
	Drivers     []RemoteDriver `json:"drivers"`
	Trucks      []RemoteTruck  `json:"trucks"`
}

type RemoteError struct {
	Status     int
	Code       string
	RetryAfter time.Duration
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("FleetScope request failed (%d, %s)", e.Status, e.Code)
}
func (e *RemoteError) Retryable() bool { return e.Status == 429 || e.Status >= 500 }

type SyncClient struct {
	Options SyncOptions
	HTTP    *http.Client
}

func NewSyncClient(o SyncOptions) *SyncClient {
	return &SyncClient{o, &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Signing is over LF-separated UTF-8 components, without a final newline.
func SyncSignature(secret, method, target, timestamp, nonce, keyID, environment, company string, body []byte) string {
	h := sha256.Sum256(body)
	canonical := strings.Join([]string{"mserp-fleet-v1", method, target, timestamp, nonce, keyID, environment, company, "fleetscope", hex.EncodeToString(h[:])}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}
func (c *SyncClient) request(ctx context.Context, method, path string, body []byte, bootstrap bool, out any) error {
	if len(body) > SyncBodyLimit {
		return errors.New("FleetScope request exceeds limit")
	}
	if !c.Options.Configured() {
		return errors.New("FleetScope sync is disabled")
	}
	key, secret := c.Options.KeyID, c.Options.Secret
	if bootstrap {
		key, secret = c.Options.BootstrapKeyID, c.Options.BootstrapSecret
		if key == "" || secret == "" {
			return errors.New("FleetScope reconciliation credentials are not configured")
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, c.Options.URL+path, bytes.NewReader(body))
	if e != nil {
		return errors.New("invalid FleetScope request")
	}
	timestamp, nonce := strconv.FormatInt(time.Now().Unix(), 10), NewID()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range map[string]string{"X-MSERP-Key-ID": key, "X-MSERP-Timestamp": timestamp, "X-MSERP-Nonce": nonce, "X-MSERP-Environment": c.Options.Environment, "X-MSERP-Company-ID": c.Options.CompanyID, "X-MSERP-Contract": ContractHash()} {
		req.Header.Set(k, v)
	}
	req.Header.Set("X-MSERP-Signature", SyncSignature(secret, method, req.URL.RequestURI(), timestamp, nonce, key, c.Options.Environment, c.Options.CompanyID, body))
	res, e := c.HTTP.Do(req)
	if e != nil {
		return &RemoteError{Status: 503, Code: "transport_error"}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusAccepted {
		return &RemoteError{Status: 502, Code: "uncommitted_response"}
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, SyncBodyLimit+1))
	if e != nil || len(data) > SyncBodyLimit {
		return &RemoteError{Status: 502, Code: "invalid_response"}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var v struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(data, &v)
		if !validToken(v.Code) {
			v.Code = "remote_error"
		}
		retry := time.Duration(0)
		if n, e := strconv.Atoi(res.Header.Get("Retry-After")); e == nil && n > 0 && n <= 3600 {
			retry = time.Duration(n) * time.Second
		}
		return &RemoteError{res.StatusCode, v.Code, retry}
	}
	if e = json.Unmarshal(data, out); e != nil {
		return &RemoteError{Status: 502, Code: "invalid_response"}
	}
	return nil
}
func (c *SyncClient) Capabilities(ctx context.Context) (Capabilities, error) {
	var v Capabilities
	e := c.request(ctx, "GET", "/capabilities", nil, false, &v)
	if e == nil && (v.Version != 1 || v.ContractHash != ContractHash() || v.Ownership != OwnershipCapability || v.CompanyID != c.Options.CompanyID || v.Environment != c.Options.Environment || !ValidID(v.Epoch) || (v.Mode != "reconcile" && v.Mode != "live")) {
		e = &RemoteError{Status: 409, Code: "contract_mismatch"}
	}
	return v, e
}
func (c *SyncClient) Snapshot(ctx context.Context) (Snapshot, error) {
	var v Snapshot
	if e := c.request(ctx, "POST", "/snapshots", []byte("{}"), false, &v); e != nil {
		return v, e
	}
	if !ValidID(v.ID) || v.CompanyID != c.Options.CompanyID || v.Environment != c.Options.Environment || !ValidID(v.Epoch) {
		return v, errors.New("invalid FleetScope snapshot identity")
	}
	seen := map[string]bool{}
	for v.NextCursor != "" {
		cursor := v.NextCursor
		if seen[cursor] || len(seen) >= 100 {
			return v, errors.New("invalid FleetScope snapshot pagination")
		}
		seen[cursor] = true
		var p Snapshot
		if e := c.request(ctx, "GET", "/snapshots/"+v.ID+"?cursor="+url.QueryEscape(cursor), nil, false, &p); e != nil {
			return v, e
		}
		if p.ID != v.ID || p.CompanyID != v.CompanyID || p.Environment != v.Environment || p.Epoch != v.Epoch || !p.CapturedAt.Equal(v.CapturedAt) {
			return v, errors.New("FleetScope snapshot changed during pagination")
		}
		v.Drivers = append(v.Drivers, p.Drivers...)
		v.Trucks = append(v.Trucks, p.Trucks...)
		v.NextCursor = p.NextCursor
		if len(v.Trucks)+len(v.Drivers) > 10000 {
			return v, errors.New("FleetScope snapshot exceeds limit")
		}
	}
	if len(v.Trucks)+len(v.Drivers) > 10000 || v.CapturedAt.Before(time.Now().Add(-15*time.Minute)) || v.CapturedAt.After(time.Now().Add(time.Minute)) {
		return v, errors.New("FleetScope snapshot is stale or exceeds limit")
	}
	ids, bindings := map[string]bool{}, map[string]bool{}
	for _, d := range v.Drivers {
		if !ValidID(d.ID) || d.Revision == "" || ids["driver:"+d.ID] || (d.MSERPID != "" && (!ValidID(d.MSERPID) || bindings["driver:"+d.MSERPID])) {
			return v, errors.New("invalid FleetScope driver snapshot")
		}
		ids["driver:"+d.ID] = true
		if d.MSERPID != "" {
			bindings["driver:"+d.MSERPID] = true
		}
	}
	for _, t := range v.Trucks {
		if !ValidID(t.ID) || t.Revision == "" || t.AssignmentRevision == "" || ids["truck:"+t.ID] || (t.MSERPID != "" && (!ValidID(t.MSERPID) || bindings["truck:"+t.MSERPID])) || (t.PrimaryID != "" && !ids["driver:"+t.PrimaryID]) || (t.SecondaryID != "" && !ids["driver:"+t.SecondaryID]) {
			return v, errors.New("invalid FleetScope truck snapshot")
		}
		ids["truck:"+t.ID] = true
		if t.MSERPID != "" {
			bindings["truck:"+t.MSERPID] = true
		}
	}
	return v, nil
}
func (c *SyncClient) Deliver(ctx context.Context, op Operation, body []byte, bootstrap bool) (Result, error) {
	var v Result
	if op.CompanyID != c.Options.CompanyID || op.Environment != c.Options.Environment {
		return v, &RemoteError{Status: 409, Code: "scope_mismatch"}
	}
	if op.Path() == "" {
		return v, errors.New("invalid FleetScope operation")
	}
	e := c.request(ctx, "POST", op.Path(), body, bootstrap, &v)
	if e == nil && (v.OperationID != op.OperationID || (v.Status != "applied" && v.Status != "duplicate") || v.CompanyID != op.CompanyID || v.Environment != op.Environment || v.Epoch != op.Epoch || v.SourceRevision != op.SourceRevision || v.AssignmentSequence != op.AssignmentSequence) {
		e = &RemoteError{Status: 502, Code: "invalid_receipt"}
	}
	return v, e
}
