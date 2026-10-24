package config

import (
	"os"
	"strings"
)

// DefaultGoogleRedirectURL is the production OAuth callback (D17).
const DefaultGoogleRedirectURL = "https://app.afh.my.id/api/auth/google/callback"

// Config is loaded entirely from the environment (set by docker-compose.yml
// / afh-infra's platform contract), never from a file. Secrets come from the
// VPS .env through compose and never appear in the repo (D17).
type Config struct {
	Port            string
	DatabaseURL     string
	PublicHostnames []string // used by urlvalidate to reject own-domain targets

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	SafeBrowsingAPIKey string

	// SignupsOpen is the D25 kill switch: only the literal "false" closes.
	SignupsOpen bool

	// AlertWebhookURL, if set, receives a plain-text POST for every re-scan
	// takedown (D27). Optional: the logs are the record either way.
	AlertWebhookURL string

	// InsecureCookies drops the __Host- prefix and Secure flag so sign-in
	// works over http://localhost. Never set in production.
	InsecureCookies bool
}

func Load() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	hostnames := os.Getenv("PUBLIC_HOSTNAMES")
	var hosts []string
	for _, h := range strings.Split(hostnames, ",") {
		if h = strings.TrimSpace(h); h != "" {
			hosts = append(hosts, h)
		}
	}

	redirect := os.Getenv("GOOGLE_REDIRECT_URL")
	if redirect == "" {
		redirect = DefaultGoogleRedirectURL
	}

	return Config{
		Port:               port,
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		PublicHostnames:    hosts,
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  redirect,
		SafeBrowsingAPIKey: os.Getenv("SAFE_BROWSING_API_KEY"),
		SignupsOpen:        os.Getenv("SIGNUPS_OPEN") != "false",
		AlertWebhookURL:    os.Getenv("ALERT_WEBHOOK_URL"),
		InsecureCookies:    os.Getenv("INSECURE_DEV_COOKIES") == "1",
	}
}

// MissingCredentials names the required secrets that are empty, so main can
// refuse to start rather than fail on the first sign-in.
func (c Config) MissingCredentials() []string {
	var missing []string
	for name, v := range map[string]string{
		"GOOGLE_CLIENT_ID":      c.GoogleClientID,
		"GOOGLE_CLIENT_SECRET":  c.GoogleClientSecret,
		"SAFE_BROWSING_API_KEY": c.SafeBrowsingAPIKey,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	return missing
}
