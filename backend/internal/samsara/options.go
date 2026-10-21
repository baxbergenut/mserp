package samsara

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type Options struct {
	Token, Version, URL string
	Interval            time.Duration
}

func LoadOptions() (Options, error) {
	o := Options{Token: strings.TrimSpace(os.Getenv("SAMSARA_API_TOKEN")), Version: strings.TrimSpace(os.Getenv("SAMSARA_API_VERSION")), URL: strings.TrimSpace(os.Getenv("SAMSARA_API_URL")), Interval: time.Minute}
	if o.URL == "" {
		o.URL = "https://api.samsara.com"
	}
	u, e := url.Parse(o.URL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return o, fmt.Errorf("SAMSARA_API_URL must be an HTTPS origin")
	}
	o.URL = strings.TrimRight(o.URL, "/")
	if strings.ContainsAny(o.Token+o.Version, "\r\n") {
		return o, fmt.Errorf("Samsara credentials contain invalid header characters")
	}
	if value := strings.TrimSpace(os.Getenv("SAMSARA_SYNC_INTERVAL")); value != "" {
		o.Interval, e = time.ParseDuration(value)
		if e != nil || o.Interval < time.Minute || o.Interval > time.Hour {
			return o, fmt.Errorf("SAMSARA_SYNC_INTERVAL must be between 1m and 1h")
		}
	}
	return o, nil
}
