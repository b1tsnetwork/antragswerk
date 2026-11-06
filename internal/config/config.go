// Package config lädt die Konfiguration (YAML + Umgebungsvariablen).
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen        string `yaml:"listen"`
	BaseURL       string `yaml:"base_url"`
	DBPath        string `yaml:"db_path"`
	Secret        string `yaml:"secret"`
	CookieSecure  bool   `yaml:"cookie_secure"`
	TrustProxy    bool   `yaml:"trust_proxy"`
	SessionHours  int    `yaml:"session_hours"`
	Demo          bool   `yaml:"demo"`
	TokenLinks    bool   `yaml:"token_links"`
	TokenDays     int    `yaml:"token_days"`
	AllowAllUsers bool   `yaml:"allow_all_users"`
	Org           string `yaml:"org"`
	LDAP          LDAP   `yaml:"ldap"`
	Roles         Roles  `yaml:"roles"`
	SMTP          SMTP   `yaml:"smtp"`
	TLS           struct {
		CertFile string `yaml:"cert_file"`
		KeyFile  string `yaml:"key_file"`
	} `yaml:"tls"`
	dir string
}

type LDAP struct {
	URL                string `yaml:"url"`
	CAFile             string `yaml:"ca_file"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
	StartTLS           bool   `yaml:"start_tls"`
	TimeoutSeconds     int    `yaml:"timeout_seconds"`
	BindDN             string `yaml:"bind_dn"`
	BindPassword       string `yaml:"bind_password"`
	BaseDN             string `yaml:"base_dn"`
	LoginFilter        string `yaml:"login_filter"`
	SearchFilter       string `yaml:"search_filter"`
	NestedGroups       bool   `yaml:"nested_groups"`
}

func (l LDAP) Timeout() time.Duration {
	if l.TimeoutSeconds <= 0 {
		return 10 * time.Second
	}
	return time.Duration(l.TimeoutSeconds) * time.Second
}

// Roles ordnet AD-Gruppen den Rollen zu. Alle übrigen angemeldeten Personen sind "nutzer".
type Roles struct {
	Admin      []string `yaml:"admin"`
	Bearbeiter []string `yaml:"bearbeiter"`
	Nutzer     []string `yaml:"nutzer"` // leer + allow_all_users = alle
}

type SMTP struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
	Security string `yaml:"security"` // starttls, tls, none
	Insecure bool   `yaml:"insecure_skip_verify"`
}

func (s SMTP) Enabled() bool { return s.Host != "" }

// Load liest die Datei; fehlt sie, wird eine Demo-Konfiguration angelegt.
func Load(path string) (*Config, bool, error) {
	c := &Config{Listen: ":8080", BaseURL: "http://localhost:8080", DBPath: "antragswerk.db", SessionHours: 10,
		TokenLinks: true, TokenDays: 30, AllowAllUsers: true, Org: "Stadtverwaltung"}
	c.LDAP.NestedGroups = true
	created := false
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c.Demo = true
		c.Secret = randSecret()
		out, _ := yaml.Marshal(c)
		hdr := "# Antragswerk – automatisch angelegte Demo-Konfiguration. Vorlage: config.example.yaml\n"
		if err := os.WriteFile(path, append([]byte(hdr), out...), 0o600); err != nil {
			return nil, false, err
		}
		created = true
	} else if err != nil {
		return nil, false, err
	} else if err := yaml.Unmarshal(b, c); err != nil {
		return nil, false, err
	}
	c.dir = filepath.Dir(path)
	env := func(k string, dst *string) {
		if v := os.Getenv(k); v != "" {
			*dst = v
		}
	}
	env("ANTRAGSWERK_SECRET", &c.Secret)
	env("ANTRAGSWERK_LDAP_BIND_PASSWORD", &c.LDAP.BindPassword)
	env("ANTRAGSWERK_SMTP_PASSWORD", &c.SMTP.Password)
	env("ANTRAGSWERK_BASE_URL", &c.BaseURL)
	if os.Getenv("ANTRAGSWERK_DEMO") == "1" {
		c.Demo = true
	}
	if c.Secret == "" {
		c.Secret = randSecret()
	}
	if c.LDAP.LoginFilter == "" {
		c.LDAP.LoginFilter = "(&(objectCategory=person)(objectClass=user)(|(sAMAccountName={u})(userPrincipalName={u})))"
	}
	if c.LDAP.SearchFilter == "" {
		c.LDAP.SearchFilter = "(&(objectCategory=person)(objectClass=user)(!(userAccountControl:1.2.840.113556.1.4.803:=2))(|(displayName=*{q}*)(sAMAccountName={q}*)(mail={q}*)(sn={q}*)(givenName={q}*)))"
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return c, created, nil
}

// Path löst relative Pfade gegen das Konfigurationsverzeichnis auf.
func (c *Config) Path(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.dir, p)
}

func randSecret() string {
	b := make([]byte, 36)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
