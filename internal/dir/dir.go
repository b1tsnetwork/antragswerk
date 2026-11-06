// Package dir bindet das Active Directory an: Anmeldung, Rollen, Personensuche. Im Demo-Modus mit eingebautem Verzeichnis.
package dir

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/go-ldap/ldap/v3"

	"antragswerk/internal/config"
)

var (
	ErrInvalid       = errors.New("benutzername oder kennwort falsch")
	ErrNotAuthorized = errors.New("keine berechtigung")
)

const oidInChain = "1.2.840.113556.1.4.1941"

// Person ist ein Eintrag im Verzeichnis.
type Person struct {
	Sam        string `json:"sam"`
	Name       string `json:"name"`
	Given      string `json:"given"`
	Surname    string `json:"surname"`
	Mail       string `json:"mail"`
	Department string `json:"department"`
	Title      string `json:"title"`
	Phone      string `json:"phone"`
	Office     string `json:"office"`
	Company    string `json:"company"`
	Manager    string `json:"manager"`
	DN         string `json:"dn"`
}

// Identity ist eine angemeldete Person.
type Identity struct {
	Person
	Role    string
	Groups  []string // DNs der Gruppen (für Stellen-Zuordnung)
	Stellen []string
}

// StelleGroup ordnet einer Stelle eine AD-Gruppe zu.
type StelleGroup struct{ Key, Group string }

type Directory struct {
	cfg  *config.Config
	tls  *tls.Config
	demo []DemoUser
	// Extra liefert zusätzliche Personen (z. B. aus dem Telefonbuch) – ergänzt die Suche und fehlende Angaben.
	Extra func(q string, limit int) []Person
}

// DemoUser ist ein Benutzer im Demo-Verzeichnis.
type DemoUser struct {
	Person
	Role    string
	Stellen []string
	Hint    string
}

func New(cfg *config.Config) (*Directory, error) {
	d := &Directory{cfg: cfg}
	if cfg.Demo {
		d.demo = demoUsers
	}
	if cfg.LDAP.URL != "" {
		tc := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.LDAP.InsecureSkipVerify} //nolint:gosec
		if h, _, err := net.SplitHostPort(strings.TrimPrefix(strings.TrimPrefix(cfg.LDAP.URL, "ldaps://"), "ldap://")); err == nil {
			tc.ServerName = h
		}
		if cfg.LDAP.CAFile != "" {
			pem, err := os.ReadFile(cfg.Path(cfg.LDAP.CAFile))
			if err != nil {
				return nil, fmt.Errorf("ldap.ca_file: %w", err)
			}
			pool, _ := x509.SystemCertPool()
			if pool == nil {
				pool = x509.NewCertPool()
			}
			pool.AppendCertsFromPEM(pem)
			tc.RootCAs = pool
		}
		d.tls = tc
	}
	return d, nil
}

func (d *Directory) LDAP() bool       { return d.cfg.LDAP.URL != "" }
func (d *Directory) Demo() []DemoUser { return d.demo }
func (d *Directory) DemoMode() bool   { return d.cfg.Demo }

func (d *Directory) dial() (*ldap.Conn, error) {
	c := d.cfg.LDAP
	conn, err := ldap.DialURL(c.URL, ldap.DialWithTLSConfig(d.tls), ldap.DialWithDialer(&net.Dialer{Timeout: c.Timeout()}))
	if err != nil {
		return nil, err
	}
	conn.SetTimeout(c.Timeout())
	if c.StartTLS && strings.HasPrefix(c.URL, "ldap://") {
		if err := conn.StartTLS(d.tls); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

func (d *Directory) service() (*ldap.Conn, error) {
	conn, err := d.dial()
	if err != nil {
		return nil, err
	}
	if err := conn.Bind(d.cfg.LDAP.BindDN, d.cfg.LDAP.BindPassword); err != nil {
		conn.Close()
		return nil, fmt.Errorf("service-bind: %w", err)
	}
	return conn, nil
}

var attrs = []string{"sAMAccountName", "displayName", "givenName", "sn", "mail", "department", "title", "telephoneNumber", "physicalDeliveryOfficeName", "company", "manager", "userAccountControl"}

func toPerson(e *ldap.Entry) Person {
	p := Person{Sam: e.GetAttributeValue("sAMAccountName"), Name: e.GetAttributeValue("displayName"), Mail: e.GetAttributeValue("mail"),
		Given: e.GetAttributeValue("givenName"), Surname: e.GetAttributeValue("sn"),
		Department: e.GetAttributeValue("department"), Title: e.GetAttributeValue("title"), Phone: e.GetAttributeValue("telephoneNumber"),
		Office: e.GetAttributeValue("physicalDeliveryOfficeName"), Company: e.GetAttributeValue("company"), DN: e.DN}
	if m := e.GetAttributeValue("manager"); m != "" {
		if dn, err := ldap.ParseDN(m); err == nil && len(dn.RDNs) > 0 && len(dn.RDNs[0].Attributes) > 0 {
			p.Manager = dn.RDNs[0].Attributes[0].Value
		}
	}
	if p.Name == "" {
		p.Name = p.Sam
	}
	return p
}

// Authenticate prüft Anmeldedaten. stellen: Stellen mit AD-Gruppe, deren Mitgliedschaft geprüft wird.
func (d *Directory) Authenticate(user, pass string, stellen []StelleGroup) (*Identity, error) {
	user = strings.TrimSpace(user)
	if i := strings.LastIndex(user, `\`); i >= 0 {
		user = user[i+1:]
	}
	if d.cfg.Demo {
		for _, u := range d.demo {
			if strings.EqualFold(u.Sam, user) {
				if pass != "demo" {
					return nil, ErrInvalid
				}
				return &Identity{Person: u.Person, Role: u.Role, Stellen: u.Stellen}, nil
			}
		}
		if !d.LDAP() {
			return nil, ErrInvalid
		}
	}
	if !d.LDAP() || user == "" || pass == "" {
		return nil, ErrInvalid
	}
	conn, err := d.service()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	filter := strings.ReplaceAll(d.cfg.LDAP.LoginFilter, "{u}", ldap.EscapeFilter(user))
	sr, err := conn.Search(ldap.NewSearchRequest(d.cfg.LDAP.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false, filter, attrs, nil))
	if err != nil {
		return nil, err
	}
	if len(sr.Entries) != 1 {
		return nil, ErrInvalid
	}
	e := sr.Entries[0]
	if uac, _ := strconv.Atoi(e.GetAttributeValue("userAccountControl")); uac&2 != 0 {
		return nil, ErrInvalid
	}
	uc, err := d.dial()
	if err != nil {
		return nil, err
	}
	defer uc.Close()
	if err := uc.Bind(e.DN, pass); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, ErrInvalid
		}
		return nil, err
	}
	id := &Identity{Person: toPerson(e)}
	in := func(groups []string) bool {
		if len(groups) == 0 {
			return false
		}
		ok, _ := d.inAny(conn, e.DN, groups)
		return ok
	}
	switch {
	case in(d.cfg.Roles.Admin):
		id.Role = "admin"
	case in(d.cfg.Roles.Bearbeiter):
		id.Role = "bearbeiter"
	case len(d.cfg.Roles.Nutzer) == 0 && d.cfg.AllowAllUsers, in(d.cfg.Roles.Nutzer):
		id.Role = "nutzer"
	default:
		return nil, ErrNotAuthorized
	}
	for _, sg := range stellen {
		if sg.Group != "" && in([]string{sg.Group}) {
			id.Stellen = append(id.Stellen, sg.Key)
		}
	}
	return id, nil
}

func (d *Directory) groupDN(conn *ldap.Conn, g string) string {
	if strings.Contains(g, "=") {
		return g
	}
	sr, err := conn.Search(ldap.NewSearchRequest(d.cfg.LDAP.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false,
		"(&(objectClass=group)(|(cn="+ldap.EscapeFilter(g)+")(sAMAccountName="+ldap.EscapeFilter(g)+")))", []string{"dn"}, nil))
	if err != nil || len(sr.Entries) == 0 {
		return ""
	}
	return sr.Entries[0].DN
}

func (d *Directory) inAny(conn *ldap.Conn, userDN string, groups []string) (bool, error) {
	for _, g := range groups {
		gdn := d.groupDN(conn, g)
		if gdn == "" {
			continue
		}
		f := "(memberOf=" + ldap.EscapeFilter(gdn) + ")"
		if d.cfg.LDAP.NestedGroups {
			f = "(memberOf:" + oidInChain + ":=" + ldap.EscapeFilter(gdn) + ")"
		}
		sr, err := conn.Search(ldap.NewSearchRequest(userDN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false, f, []string{"dn"}, nil))
		if err != nil {
			continue
		}
		if len(sr.Entries) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// Search sucht Personen (für Personenfelder).
func (d *Directory) Search(q string, limit int) ([]Person, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 {
		return nil, nil
	}
	var out []Person
	if d.cfg.Demo {
		lq := strings.ToLower(q)
		for _, u := range append(demoUsers, demoExtra...) {
			if strings.Contains(strings.ToLower(u.Name+" "+u.Sam+" "+u.Mail+" "+u.Department), lq) {
				out = append(out, u.Person)
			}
		}
		if !d.LDAP() {
			return d.merge(out, q, limit), nil
		}
	}
	if !d.LDAP() {
		return d.merge(out, q, limit), nil
	}
	conn, err := d.service()
	if err != nil {
		return out, err
	}
	defer conn.Close()
	filter := strings.ReplaceAll(d.cfg.LDAP.SearchFilter, "{q}", ldap.EscapeFilter(q))
	sr, err := conn.Search(ldap.NewSearchRequest(d.cfg.LDAP.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, limit, 0, false, filter, attrs, nil))
	if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
		return out, err
	}
	if sr != nil {
		for _, e := range sr.Entries {
			out = append(out, toPerson(e))
		}
	}
	return d.merge(out, q, limit), nil
}

// merge ergänzt Treffer aus Zusatzquellen und füllt leere Felder (Telefon, Raum, Funktion …).
func (d *Directory) merge(out []Person, q string, limit int) []Person {
	if d.Extra != nil {
		key := func(p Person) string {
			if p.Sam != "" {
				return strings.ToLower(p.Sam)
			}
			return strings.ToLower(p.Mail)
		}
		idx := map[string]int{}
		for i, p := range out {
			idx[key(p)] = i
			if p.Mail != "" {
				idx[strings.ToLower(p.Mail)] = i
			}
		}
		for _, x := range d.Extra(q, limit) {
			i, ok := idx[key(x)]
			if !ok && x.Mail != "" {
				i, ok = idx[strings.ToLower(x.Mail)]
			}
			if !ok {
				out = append(out, x)
				continue
			}
			p := &out[i]
			fill := func(dst *string, v string) {
				if *dst == "" {
					*dst = v
				}
			}
			fill(&p.Phone, x.Phone)
			fill(&p.Office, x.Office)
			fill(&p.Title, x.Title)
			fill(&p.Department, x.Department)
			fill(&p.Given, x.Given)
			fill(&p.Surname, x.Surname)
			fill(&p.Mail, x.Mail)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ---------- Demo-Verzeichnis ----------

func p(sam, name, dept, title, phone, office string) Person {
	parts := strings.SplitN(name, " ", 2)
	return Person{Sam: sam, Name: name, Given: parts[0], Surname: parts[len(parts)-1], Mail: sam + "@demo-stadt.de", Department: dept, Title: title, Phone: phone, Office: office, Company: "Stadtverwaltung"}
}

var demoUsers = []DemoUser{
	{Person: p("a.becker", "Anna Becker", "Personal (10)", "Sachbearbeiterin Personal", "-1012", "Rathaus A 1.12"), Role: "nutzer", Stellen: []string{"personal"},
		Hint: "Personalabteilung – stellt Eintritts-, Wechsel- und Austrittsanträge"},
	{Person: p("m.schulz", "Markus Schulz", "Bürgerservice (32)", "Abteilungsleiter Bürgerservice", "-3200", "Rathaus B 0.04"), Role: "nutzer",
		Hint: "Vorgesetzter – gibt Anträge seiner Abteilung frei"},
	{Person: p("s.wagner", "Sabine Wagner", "Kämmerei (20)", "Fachbereichsleiterin Finanzen", "-2000", "Rathaus A 2.01"), Role: "nutzer",
		Hint: "Verfahrensverantwortliche Finanzsoftware & Dateneigentümerin Kämmerei"},
	{Person: p("t.klein", "Thomas Klein", "IT (11)", "Systemadministrator", "-1150", "Rathaus C 1.03"), Role: "bearbeiter", Stellen: []string{"it_benutzer", "it_client"},
		Hint: "IT-Benutzerverwaltung – AD-Konten, Gruppen, Clients"},
	{Person: p("j.hoffmann", "Julia Hoffmann", "IT (11)", "Exchange-/M365-Administratorin", "-1151", "Rathaus C 1.04"), Role: "bearbeiter", Stellen: []string{"it_mail", "it_telefonie"},
		Hint: "Exchange & Telefonie"},
	{Person: p("k.richter", "Klaus Richter", "Zentrale Dienste (10.3)", "Hausverwaltung", "-1030", "Rathaus A 0.10"), Role: "nutzer", Stellen: []string{"hausverwaltung"},
		Hint: "Räume, Schlüssel, Zutrittskarten"},
	{Person: p("l.weber", "Laura Weber", "Ordnungsamt (32.1)", "Fachverfahrensbetreuerin", "-3210", "Rathaus B 1.07"), Role: "nutzer", Stellen: []string{"fv_betreuung"},
		Hint: "Verfahrensverantwortliche Einwohnermeldewesen & Gewerbe"},
	{Person: p("admin", "Admin (Demo)", "IT (11)", "Leitung IT", "-1100", "Rathaus C 1.01"), Role: "admin", Stellen: []string{"it_benutzer", "it_mail", "it_client", "it_telefonie", "isb"},
		Hint: "Administrator – Designer, Stammdaten, alles"},
}

var demoExtra = []DemoUser{
	{Person: p("p.neumann", "Petra Neumann", "Bürgerservice (32)", "Sachbearbeiterin Meldewesen", "-3221", "Rathaus B 0.11")},
	{Person: p("f.krause", "Frank Krause", "Kämmerei (20)", "Kassenleiter", "-2050", "Rathaus A 2.08")},
	{Person: p("m.lange", "Miriam Lange", "Jugendamt (51)", "Amtsleiterin Jugendamt", "-5100", "Haus Jugend 2.01")},
	{Person: p("d.yilmaz", "Deniz Yılmaz", "Bauamt (60)", "Bauingenieur", "-6012", "Technisches Rathaus 3.12")},
	{Person: p("c.fischer", "Christina Fischer", "Bauamt (60)", "Amtsleiterin Bauamt", "-6000", "Technisches Rathaus 3.01")},
	{Person: p("r.mueller", "René Müller", "Ordnungsamt (32.1)", "Abteilungsleiter Ordnungsamt", "-3100", "Rathaus B 1.01")},
}
