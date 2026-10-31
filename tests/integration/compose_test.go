// Package integration holds tests that span components. This file checks the
// isolation rules of the demo stack statically, so they hold without Docker.
package integration

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type service struct {
	Environment map[string]string `yaml:"environment"`
	Ports       []string          `yaml:"ports"`
	DependsOn   map[string]any    `yaml:"depends_on"`
	Tmpfs       []string          `yaml:"tmpfs"`
	Image       string            `yaml:"image"`
}

func loadServices(t *testing.T) map[string]service {
	t.Helper()
	data, err := os.ReadFile("../../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Services map[string]service `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file.Services
}

// ADR 0003: replay comparisons are meaningless if two backends share state.
func TestEachBackendOwnsItsDatabase(t *testing.T) {
	services := loadServices(t)
	seen := map[string]string{}
	for _, name := range []string{"baseline", "candidate", "fixed"} {
		svc, ok := services[name]
		if !ok {
			t.Fatalf("service %q is missing", name)
		}
		dsn, err := url.Parse(svc.Environment["DATABASE_URL"])
		if err != nil {
			t.Fatal(err)
		}
		host := dsn.Hostname()
		if want := "postgres-" + name; host != want {
			t.Errorf("%s uses database %q, want %q", name, host, want)
		}
		if other, dup := seen[host]; dup {
			t.Errorf("%s and %s share database %q", name, other, host)
		}
		seen[host] = name
		if _, ok := svc.DependsOn[host]; !ok {
			t.Errorf("%s does not wait for %s", name, host)
		}
		db, ok := services[host]
		if !ok {
			t.Fatalf("database service %q is missing", host)
		}
		if !strings.HasPrefix(db.Image, "postgres:16") {
			t.Errorf("%s image = %q", host, db.Image)
		}
		if len(db.Tmpfs) == 0 {
			t.Errorf("%s must use tmpfs so restarts return to the seed", host)
		}
	}
}

func TestPublishedPortsAreLoopbackOnly(t *testing.T) {
	for name, svc := range loadServices(t) {
		for _, port := range svc.Ports {
			if !strings.HasPrefix(port, "127.0.0.1:") {
				t.Errorf("%s publishes %q beyond loopback", name, port)
			}
		}
		if strings.HasPrefix(name, "postgres-") && len(svc.Ports) > 0 {
			t.Errorf("%s must not publish ports", name)
		}
	}
}
