// Package store kapselt die SQLite-Datenbank.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"antragswerk/internal/flow"
)

var ErrNotFound = errors.New("nicht gefunden")

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS docs (kind TEXT NOT NULL, key TEXT NOT NULL, json TEXT NOT NULL, updated_at TIMESTAMP, updated_by TEXT, sort INTEGER DEFAULT 0, PRIMARY KEY(kind, key));
CREATE TABLE IF NOT EXISTS workflows (id INTEGER PRIMARY KEY, key TEXT UNIQUE NOT NULL, archived INTEGER DEFAULT 0, sort INTEGER DEFAULT 0, seed_rev INTEGER DEFAULT 0, created_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS wf_versions (id INTEGER PRIMARY KEY, wf_id INTEGER NOT NULL, version INTEGER NOT NULL, status TEXT NOT NULL, def TEXT NOT NULL, note TEXT DEFAULT '',
  created_at TIMESTAMP, created_by TEXT, published_at TIMESTAMP, published_by TEXT DEFAULT '');
CREATE INDEX IF NOT EXISTS wfv_wf ON wf_versions(wf_id);
CREATE TABLE IF NOT EXISTS counters (k TEXT PRIMARY KEY, n INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS requests (id INTEGER PRIMARY KEY, number TEXT UNIQUE, wf_id INTEGER, version_id INTEGER, wf_key TEXT, wf_name TEXT, title TEXT, status TEXT,
  data TEXT, requester TEXT, requester_name TEXT, requester_mail TEXT, created_at TIMESTAMP, updated_at TIMESTAMP, closed_at TIMESTAMP);
CREATE INDEX IF NOT EXISTS req_status ON requests(status);
CREATE TABLE IF NOT EXISTS instances (id INTEGER PRIMARY KEY, request_id INTEGER NOT NULL, step_id TEXT, item INTEGER, item_label TEXT, kind TEXT, title TEXT, state TEXT,
  assignee TEXT, token TEXT, due_at TIMESTAMP, created_at TIMESTAMP, done_at TIMESTAMP, done_by TEXT DEFAULT '', done_by_name TEXT DEFAULT '', comment TEXT DEFAULT '',
  outputs TEXT DEFAULT '{}', checks TEXT DEFAULT '[]', reminded INTEGER DEFAULT 0);
CREATE INDEX IF NOT EXISTS inst_req ON instances(request_id);
CREATE INDEX IF NOT EXISTS inst_state ON instances(state);
CREATE UNIQUE INDEX IF NOT EXISTS inst_token ON instances(token) WHERE token <> '';
CREATE TABLE IF NOT EXISTS journal (id INTEGER PRIMARY KEY, request_id INTEGER, instance_id INTEGER, at TIMESTAMP, actor TEXT, actor_name TEXT, kind TEXT, msg TEXT, detail TEXT DEFAULT '');
CREATE INDEX IF NOT EXISTS j_req ON journal(request_id);
CREATE TABLE IF NOT EXISTS mails (id INTEGER PRIMARY KEY, request_id INTEGER, instance_id INTEGER, at TIMESTAMP, to_addr TEXT, subject TEXT, body TEXT, link TEXT DEFAULT '', status TEXT, error TEXT DEFAULT '');
CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, username TEXT, display TEXT, mail TEXT, role TEXT, stellen TEXT, csrf TEXT, expires_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS audit (id INTEGER PRIMARY KEY, at TIMESTAMP, actor TEXT, ip TEXT, action TEXT, detail TEXT);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(8000)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	// Ergänzungen späterer Versionen (Fehler = Spalte existiert bereits)
	for _, m := range []string{
		`ALTER TABLE workflows ADD COLUMN disabled_note TEXT DEFAULT ''`,
		`ALTER TABLE workflows ADD COLUMN disabled_at TIMESTAMP`,
		`ALTER TABLE workflows ADD COLUMN disabled_by TEXT DEFAULT ''`,
	} {
		_, _ = db.Exec(m)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func js(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ---------- Dokumente (Kataloge, Stellen, Variablen, Bausteine) ----------

func (s *Store) PutDoc(ctx context.Context, kind, key string, v any, by string, sort int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO docs(kind,key,json,updated_at,updated_by,sort) VALUES(?,?,?,?,?,?)
	  ON CONFLICT(kind,key) DO UPDATE SET json=excluded.json, updated_at=excluded.updated_at, updated_by=excluded.updated_by, sort=excluded.sort`,
		kind, key, js(v), time.Now(), by, sort)
	return err
}

// CountDocs zählt die Dokumente einer Art.
func (s *Store) CountDocs(ctx context.Context, kind string) int {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM docs WHERE kind=?`, kind).Scan(&n)
	return n
}

// RawDocs liefert die JSON-Inhalte einer Art.
func (s *Store) RawDocs(ctx context.Context, kind string) []string {
	rows, err := s.db.QueryContext(ctx, `SELECT json FROM docs WHERE kind=? ORDER BY sort, key`, kind)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) == nil {
			out = append(out, raw)
		}
	}
	return out
}

// SearchPersons durchsucht die aus Datenquellen übernommenen Personen.
func (s *Store) SearchPersons(ctx context.Context, q string, limit int) []map[string]string {
	rows, err := s.db.QueryContext(ctx, `SELECT json FROM docs WHERE kind LIKE 'person:%' AND LOWER(json) LIKE ? LIMIT ?`, "%"+strings.ToLower(q)+"%", limit*3)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]string
	lq := strings.ToLower(q)
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) != nil {
			continue
		}
		var m map[string]string
		if json.Unmarshal([]byte(raw), &m) != nil {
			continue
		}
		if strings.Contains(strings.ToLower(m["name"]+" "+m["sam"]+" "+m["mail"]+" "+m["department"]+" "+m["given"]+" "+m["surname"]), lq) {
			out = append(out, m)
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

// GetDoc liest ein einzelnes Dokument (false, wenn es fehlt).
func (s *Store) GetDoc(ctx context.Context, kind, key string, v any) bool {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT json FROM docs WHERE kind=? AND key=?`, kind, key).Scan(&raw); err != nil {
		return false
	}
	return json.Unmarshal([]byte(raw), v) == nil
}

func (s *Store) DeleteDoc(ctx context.Context, kind, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM docs WHERE kind=? AND key=?`, kind, key)
	return err
}

func (s *Store) HasDocs(ctx context.Context, kind string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM docs WHERE kind=?`, kind).Scan(&n)
	return n > 0
}

func listDocs[T any](ctx context.Context, s *Store, kind string) ([]T, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT json FROM docs WHERE kind=? ORDER BY sort, key`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal([]byte(raw), &v); err == nil {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

// SeedKeys merkt sich, welche mitgelieferten Schlüssel bereits angeboten wurden.
func (s *Store) SeedKeys(ctx context.Context, name string) []string {
	var raw string
	_ = s.db.QueryRowContext(ctx, `SELECT json FROM docs WHERE kind='seed' AND key=?`, name).Scan(&raw)
	var out []string
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func (s *Store) AddSeedKeys(ctx context.Context, name string, keys []string) {
	have := map[string]bool{}
	out := s.SeedKeys(ctx, name)
	for _, k := range out {
		have[k] = true
	}
	for _, k := range keys {
		if !have[k] {
			out = append(out, k)
		}
	}
	_ = s.PutDoc(ctx, "seed", name, out, "system", 0)
}

func (s *Store) Catalogs(ctx context.Context) ([]flow.Catalog, error) {
	return listDocs[flow.Catalog](ctx, s, "catalog")
}
func (s *Store) Stellen(ctx context.Context) ([]flow.Stelle, error) {
	return listDocs[flow.Stelle](ctx, s, "stelle")
}
func (s *Store) Snippets(ctx context.Context) ([]flow.Snippet, error) {
	return listDocs[flow.Snippet](ctx, s, "snippet")
}

// Var ist eine globale Variable ({{.var.key}}).
type Var struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

func (s *Store) Vars(ctx context.Context) ([]Var, error) { return listDocs[Var](ctx, s, "var") }

func (s *Store) ReplaceDocs(ctx context.Context, kind string, keys []string, vals []any, by string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM docs WHERE kind=?`, kind); err != nil {
		return err
	}
	for i, k := range keys {
		if _, err := tx.ExecContext(ctx, `INSERT INTO docs(kind,key,json,updated_at,updated_by,sort) VALUES(?,?,?,?,?,?)`, kind, k, js(vals[i]), time.Now(), by, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- Workflows ----------

type Workflow struct {
	ID           int64
	Key          string
	Archived     bool // deaktiviert: kann nicht mehr beantragt werden, laufende Vorgänge laufen weiter
	DisabledNote string
	DisabledAt   sql.NullTime
	DisabledBy   string
	Sort         int
	SeedRev      int
	Active       *Version // veröffentlichte Version
	Draft        *Version // Entwurf
	Requests     int
	Open         int
}

// Current ist der Stand, den der Designer zeigt (Entwurf vor aktiv).
func (w *Workflow) Current() *Version {
	if w.Draft != nil {
		return w.Draft
	}
	return w.Active
}

type Version struct {
	ID          int64
	WfID        int64
	Version     int
	Status      string
	Def         flow.Def
	Note        string
	CreatedAt   time.Time
	CreatedBy   string
	PublishedAt sql.NullTime
	PublishedBy string
}

func scanVersion(sc interface{ Scan(...any) error }) (*Version, error) {
	v := &Version{}
	var raw string
	if err := sc.Scan(&v.ID, &v.WfID, &v.Version, &v.Status, &raw, &v.Note, &v.CreatedAt, &v.CreatedBy, &v.PublishedAt, &v.PublishedBy); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &v.Def); err != nil {
		return nil, err
	}
	return v, nil
}

const verCols = `id, wf_id, version, status, def, note, created_at, created_by, published_at, published_by`

func (s *Store) Workflows(ctx context.Context, withArchived bool) ([]*Workflow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.id, w.key, w.archived, w.sort, w.seed_rev, COALESCE(w.disabled_note,''), w.disabled_at, COALESCE(w.disabled_by,''),
	  (SELECT COUNT(*) FROM requests r WHERE r.wf_id=w.id), (SELECT COUNT(*) FROM requests r WHERE r.wf_id=w.id AND r.status IN ('laufend','rueckfrage'))
	  FROM workflows w ORDER BY w.sort, w.id`)
	if err != nil {
		return nil, err
	}
	var out []*Workflow
	for rows.Next() {
		w := &Workflow{}
		if err := rows.Scan(&w.ID, &w.Key, &w.Archived, &w.Sort, &w.SeedRev, &w.DisabledNote, &w.DisabledAt, &w.DisabledBy, &w.Requests, &w.Open); err != nil {
			rows.Close()
			return nil, err
		}
		if w.Archived && !withArchived {
			continue
		}
		out = append(out, w)
	}
	rows.Close()
	for _, w := range out {
		if err := s.loadVersions(ctx, w); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) loadVersions(ctx context.Context, w *Workflow) error {
	rows, err := s.db.QueryContext(ctx, `SELECT `+verCols+` FROM wf_versions WHERE wf_id=? AND status IN ('aktiv','entwurf')`, w.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return err
		}
		if v.Status == "aktiv" {
			w.Active = v
		} else {
			w.Draft = v
		}
	}
	return rows.Err()
}

func (s *Store) Workflow(ctx context.Context, key string) (*Workflow, error) {
	w := &Workflow{}
	err := s.db.QueryRowContext(ctx, `SELECT id, key, archived, sort, seed_rev, COALESCE(disabled_note,''), disabled_at, COALESCE(disabled_by,'') FROM workflows WHERE key=?`, key).
		Scan(&w.ID, &w.Key, &w.Archived, &w.Sort, &w.SeedRev, &w.DisabledNote, &w.DisabledAt, &w.DisabledBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return w, s.loadVersions(ctx, w)
}

func (s *Store) Versions(ctx context.Context, wfID int64) ([]*Version, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+verCols+` FROM wf_versions WHERE wf_id=? ORDER BY CASE status WHEN 'entwurf' THEN 0 ELSE 1 END, version DESC`, wfID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) VersionByID(ctx context.Context, id int64) (*Version, error) {
	v, err := scanVersion(s.db.QueryRowContext(ctx, `SELECT `+verCols+` FROM wf_versions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

// CreateWorkflow legt einen Workflow an; publish=true veröffentlicht direkt Version 1.
func (s *Store) CreateWorkflow(ctx context.Context, d flow.Def, by string, publish bool, seedRev int) (*Workflow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var sortN int
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort),0)+1 FROM workflows`).Scan(&sortN)
	res, err := tx.ExecContext(ctx, `INSERT INTO workflows(key, sort, seed_rev, created_at) VALUES(?,?,?,?)`, d.Key, sortN, seedRev, time.Now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("Schlüssel %q ist bereits vergeben", d.Key)
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	now := time.Now()
	if publish {
		_, err = tx.ExecContext(ctx, `INSERT INTO wf_versions(wf_id,version,status,def,note,created_at,created_by,published_at,published_by) VALUES(?,?,?,?,?,?,?,?,?)`,
			id, 1, "aktiv", js(d), "Erstfassung", now, by, now, by)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO wf_versions(wf_id,version,status,def,created_at,created_by) VALUES(?,?,?,?,?,?)`, id, 0, "entwurf", js(d), now, by)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Workflow(ctx, d.Key)
}

func (s *Store) SaveDraft(ctx context.Context, w *Workflow, d flow.Def, by string) error {
	if w.Draft != nil {
		_, err := s.db.ExecContext(ctx, `UPDATE wf_versions SET def=?, created_at=?, created_by=? WHERE id=?`, js(d), time.Now(), by, w.Draft.ID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO wf_versions(wf_id,version,status,def,created_at,created_by) VALUES(?,?,?,?,?,?)`, w.ID, 0, "entwurf", js(d), time.Now(), by)
	return err
}

func (s *Store) DiscardDraft(ctx context.Context, w *Workflow) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wf_versions WHERE wf_id=? AND status='entwurf'`, w.ID)
	return err
}

func (s *Store) Publish(ctx context.Context, w *Workflow, note, by string) (int, error) {
	if w.Draft == nil {
		return 0, errors.New("kein Entwurf vorhanden")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var n int
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM wf_versions WHERE wf_id=?`, w.ID).Scan(&n)
	if _, err := tx.ExecContext(ctx, `UPDATE wf_versions SET status='abgeloest' WHERE wf_id=? AND status='aktiv'`, w.ID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wf_versions SET status='aktiv', version=?, note=?, published_at=?, published_by=? WHERE id=?`, n, note, time.Now(), by, w.Draft.ID); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// SetDisabled deaktiviert bzw. aktiviert einen Ablauf für neue Anträge.
func (s *Store) SetDisabled(ctx context.Context, w *Workflow, off bool, note, by string) error {
	var at any
	if off {
		at = time.Now()
	} else {
		note, by = "", ""
	}
	_, err := s.db.ExecContext(ctx, `UPDATE workflows SET archived=?, disabled_note=?, disabled_at=?, disabled_by=? WHERE id=?`, off, note, at, by, w.ID)
	return err
}

func (s *Store) SetSeedRev(ctx context.Context, w *Workflow, rev int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workflows SET seed_rev=? WHERE id=?`, rev, w.ID)
	return err
}

func (s *Store) SetSort(ctx context.Context, keys []string) error {
	for i, k := range keys {
		if _, err := s.db.ExecContext(ctx, `UPDATE workflows SET sort=? WHERE key=?`, i+1, k); err != nil {
			return err
		}
	}
	return nil
}

// ---------- Sitzungen & Audit ----------

type Session struct {
	TokenHash string
	Username  string
	Display   string
	Mail      string
	Role      string
	Stellen   []string
	CSRF      string
	ExpiresAt time.Time
}

func (s *Store) CreateSession(ctx context.Context, se Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions VALUES(?,?,?,?,?,?,?,?)`, se.TokenHash, se.Username, se.Display, se.Mail, se.Role, js(se.Stellen), se.CSRF, se.ExpiresAt)
	return err
}

func (s *Store) GetSession(ctx context.Context, hash string) (*Session, error) {
	se := &Session{}
	var st string
	err := s.db.QueryRowContext(ctx, `SELECT token_hash, username, display, mail, role, stellen, csrf, expires_at FROM sessions WHERE token_hash=?`, hash).
		Scan(&se.TokenHash, &se.Username, &se.Display, &se.Mail, &se.Role, &st, &se.CSRF, &se.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(se.ExpiresAt) {
		_ = s.DeleteSession(ctx, hash)
		return nil, ErrNotFound
	}
	_ = json.Unmarshal([]byte(st), &se.Stellen)
	return se, nil
}

func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, hash)
	return err
}

func (s *Store) PurgeSessions(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, time.Now())
}

type AuditEntry struct {
	At     time.Time
	Actor  string
	IP     string
	Action string
	Detail string
}

func (s *Store) Audit(ctx context.Context, a AuditEntry) {
	_, _ = s.db.ExecContext(ctx, `INSERT INTO audit(at,actor,ip,action,detail) VALUES(?,?,?,?,?)`, time.Now(), a.Actor, a.IP, a.Action, a.Detail)
}

func (s *Store) AuditLog(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT at, actor, ip, action, detail FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.At, &a.Actor, &a.IP, &a.Action, &a.Detail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
