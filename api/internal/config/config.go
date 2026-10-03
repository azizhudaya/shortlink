package config

import (
	"os"
	"strings"
)

// Config is loaded entirely from the environment (set by docker-compose.yml
// / afh-infra's platform contract), never from a file — there is nothing
// here sensitive enough to warrant one and nothing here that changes
// per-deploy beyond what compose already injects.
type Config struct {
	Port            string
	DatabaseURL     string
	PublicHostnames []string // used by urlvalidate to reject own-domain targets
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

	return Config{
		Port:            port,
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		PublicHostnames: hosts,
	}
}
