package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"antragswerk/internal/flow"
)

// Request ist ein Vorgang (gestellter Antrag).
type Request struct {
	ID            int64
	Number        string
	WfID          int64
	VersionID     int64
	WfKey         string
	WfName        string
	Title         string
	Status        string // laufend, rueckfrage, abgeschlossen, abgelehnt, zurueckgezogen
	Data          map[string]any
	Requester     string
	RequesterName string
	RequesterMail string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ClosedAt      sql.NullTime
	// berechnet
	Open, Total, DoneN int
	Overdue            bool
	Current            string
	OpenApproval       bool
	OpenTask           bool
	OpenWait           bool
	Assignees          []string
}

// Phase ordnet einen Vorgang einer Kanban-Spalte zu.
func (r *Request) Phase() string {
	switch {
	case r.Status == "rueckfrage":
		return "rueckfrage"
	case r.Status != "laufend":
		return "erledigt"
	case r.OpenApproval:
		return "freigabe"
	case r.OpenTask:
		return "umsetzung"
	case r.OpenWait:
		return "termin"
	}
	return "umsetzung"
}

// Instance ist ein laufender oder erledigter Schritt eines Vorgangs.
type Instance struct {
	ID         int64
	RequestID  int64
	StepID     string
	Item       int
	ItemLabel  string
	Kind       string
	Title      string
	State      string // offen, erledigt, freigegeben, abgelehnt, rueckfrage, uebersprungen, abgebrochen, geplant, versendet
	Assignee   flow.Assignee
	Token      string
	DueAt      sql.NullTime
	CreatedAt  time.Time
	DoneAt     sql.NullTime
	DoneBy     string
	DoneByName string
	Comment    string
	Outputs    map[string]any
	Checks     []bool
	Reminded   bool
	// für Listen
	ReqNumber string
	ReqTitle  string
	ReqWf     string
}

// Terminal: Schritt ist abgeschlossen (positiv oder übersprungen).
func (i *Instance) Passed() bool {
	switch i.State {
	case "erledigt", "freigegeben", "uebersprungen", "versendet":
		return true
	}
	return false
}

func (i *Instance) Active() bool {
	return i.State == "offen" || i.State == "rueckfrage" || i.State == "geplant" || i.State == "extern"
}

func (i *Instance) Overdue() bool {
	return i.State == "offen" && i.DueAt.Valid && time.Now().After(i.DueAt.Time)
}

const reqCols = `id, number, wf_id, version_id, wf_key, wf_name, title, status, data, requester, requester_name, requester_mail, created_at, updated_at, closed_at`

func scanReq(sc interface{ Scan(...any) error }) (*Request, error) {
	r := &Request{}
	var raw string
	if err := sc.Scan(&r.ID, &r.Number, &r.WfID, &r.VersionID, &r.WfKey, &r.WfName, &r.Title, &r.Status, &raw, &r.Requester, &r.RequesterName, &r.RequesterMail,
		&r.CreatedAt, &r.UpdatedAt, &r.ClosedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(raw), &r.Data)
	if r.Data == nil {
		r.Data = map[string]any{}
	}
	return r, nil
}

// NextNumber vergibt PREFIX-JJJJ-NNNN.
func (s *Store) nextNumber(ctx context.Context, tx *sql.Tx, prefix string) (string, error) {
	if prefix == "" {
		prefix = "AW"
	}
	y := time.Now().Year()
	k := fmt.Sprintf("%s-%d", prefix, y)
	var n int
	err := tx.QueryRowContext(ctx, `INSERT INTO counters(k,n) VALUES(?,1) ON CONFLICT(k) DO UPDATE SET n=n+1 RETURNING n`, k).Scan(&n)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%04d", k, n), nil
}

func (s *Store) CreateRequest(ctx context.Context, r *Request, prefix string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	num, err := s.nextNumber(ctx, tx, prefix)
	if err != nil {
		return err
	}
	now := time.Now()
	res, err := tx.ExecContext(ctx, `INSERT INTO requests(number,wf_id,version_id,wf_key,wf_name,title,status,data,requester,requester_name,requester_mail,created_at,updated_at)
	  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, num, r.WfID, r.VersionID, r.WfKey, r.WfName, r.Title, r.Status, js(r.Data), r.Requester, r.RequesterName, r.RequesterMail, now, now)
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	r.Number, r.CreatedAt, r.UpdatedAt = num, now, now
	return tx.Commit()
}

func (s *Store) Request(ctx context.Context, id int64) (*Request, error) {
	r, err := scanReq(s.db.QueryRowContext(ctx, `SELECT `+reqCols+` FROM requests WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

func (s *Store) UpdateRequest(ctx context.Context, r *Request) error {
	r.UpdatedAt = time.Now()
	_, err := s.db.ExecContext(ctx, `UPDATE requests SET title=?, status=?, data=?, updated_at=?, closed_at=? WHERE id=?`,
		r.Title, r.Status, js(r.Data), r.UpdatedAt, r.ClosedAt, r.ID)
	return err
}

// RequestFilter für Listen.
type RequestFilter struct {
	Q         string
	Status    string // "", offen, laufend, rueckfrage, abgeschlossen, abgelehnt, zurueckgezogen
	WfKey     string
	Requester string // nur eigene
	VisibleTo *Visibility
	Limit     int
	Offset    int
}

// Visibility schränkt auf Vorgänge ein, die eine Person sehen darf (eigene oder zugewiesene).
type Visibility struct {
	Username string
	Mail     string
	Stellen  []string
}

func (s *Store) Requests(ctx context.Context, f RequestFilter) ([]*Request, int, error) {
	var where []string
	var args []any
	if f.Q != "" {
		where = append(where, `(number LIKE ? OR title LIKE ? OR requester_name LIKE ? OR data LIKE ?)`)
		q := "%" + f.Q + "%"
		args = append(args, q, q, q, q)
	}
	switch f.Status {
	case "":
	case "offen":
		where = append(where, `status IN ('laufend','rueckfrage')`)
	default:
		where = append(where, `status=?`)
		args = append(args, f.Status)
	}
	if f.WfKey != "" {
		where = append(where, `wf_key=?`)
		args = append(args, f.WfKey)
	}
	if f.Requester != "" {
		where = append(where, `requester=?`)
		args = append(args, f.Requester)
	}
	if v := f.VisibleTo; v != nil {
		w, a := visClause(v)
		where = append(where, `(requester=? OR id IN (SELECT request_id FROM instances WHERE `+w+`))`)
		args = append(append(args, v.Username), a...)
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM requests`+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+reqCols+` FROM requests`+cond+` ORDER BY CASE WHEN status IN ('laufend','rueckfrage') THEN 0 ELSE 1 END, updated_at DESC LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	var out []*Request
	for rows.Next() {
		r, err := scanReq(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, r)
	}
	rows.Close()
	if err := s.fillProgress(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *Store) fillProgress(ctx context.Context, rs []*Request) error {
	if len(rs) == 0 {
		return nil
	}
	ids := make([]string, len(rs))
	by := map[int64]*Request{}
	for i, r := range rs {
		ids[i] = fmt.Sprint(r.ID)
		by[r.ID] = r
	}
	rows, err := s.db.QueryContext(ctx, `SELECT request_id, state, title, item_label, due_at, kind, assignee FROM instances WHERE request_id IN (`+strings.Join(ids, ",")+`) ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var st, title, item, kind, as string
		var due sql.NullTime
		if err := rows.Scan(&id, &st, &title, &item, &due, &kind, &as); err != nil {
			return err
		}
		r := by[id]
		if st == "uebersprungen" || st == "abgebrochen" {
			continue
		}
		r.Total++
		switch st {
		case "offen", "rueckfrage", "geplant", "extern":
			r.Open++
			if r.Current == "" {
				r.Current = title
				if item != "" {
					r.Current += " · " + item
				}
			} else if !strings.HasSuffix(r.Current, "…") && r.Open == 2 {
				r.Current += " …"
			}
			if st == "offen" && due.Valid && time.Now().After(due.Time) {
				r.Overdue = true
			}
			switch {
			case st == "geplant":
				r.OpenWait = true
			case kind == "freigabe":
				r.OpenApproval = true
			default:
				r.OpenTask = true
			}
			if st == "offen" {
				var a struct {
					Label string `json:"label"`
				}
				_ = json.Unmarshal([]byte(as), &a)
				if a.Label != "" && len(r.Assignees) < 3 && !containsStr(r.Assignees, a.Label) {
					r.Assignees = append(r.Assignees, a.Label)
				}
			}
		default:
			r.DoneN++
		}
	}
	return rows.Err()
}

func visClause(v *Visibility) (string, []any) {
	parts := []string{`assignee LIKE ?`}
	args := []any{`%"` + v.Username + `"%`}
	if v.Mail != "" {
		parts = append(parts, `LOWER(assignee) LIKE ?`)
		args = append(args, `%"`+strings.ToLower(v.Mail)+`"%`)
	}
	for _, st := range v.Stellen {
		parts = append(parts, `assignee LIKE ?`)
		args = append(args, `%"stellen":[%"`+st+`"%`)
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// CountByStatus für die Übersicht.
func (s *Store) CountByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM requests GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// ---------- Instanzen ----------

const instCols = `i.id, i.request_id, i.step_id, i.item, i.item_label, i.kind, i.title, i.state, i.assignee, i.token, i.due_at, i.created_at, i.done_at, i.done_by, i.done_by_name, i.comment, i.outputs, i.checks, i.reminded`

func scanInst(sc interface{ Scan(...any) error }, extra ...any) (*Instance, error) {
	in := &Instance{}
	var as, outs, checks string
	dst := []any{&in.ID, &in.RequestID, &in.StepID, &in.Item, &in.ItemLabel, &in.Kind, &in.Title, &in.State, &as, &in.Token, &in.DueAt, &in.CreatedAt,
		&in.DoneAt, &in.DoneBy, &in.DoneByName, &in.Comment, &outs, &checks, &in.Reminded}
	if err := sc.Scan(append(dst, extra...)...); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(as), &in.Assignee)
	_ = json.Unmarshal([]byte(outs), &in.Outputs)
	_ = json.Unmarshal([]byte(checks), &in.Checks)
	if in.Outputs == nil {
		in.Outputs = map[string]any{}
	}
	return in, nil
}

func (s *Store) Instances(ctx context.Context, reqID int64) ([]*Instance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+instCols+` FROM instances i WHERE request_id=? ORDER BY id`, reqID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Instance
	for rows.Next() {
		in, err := scanInst(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (s *Store) Instance(ctx context.Context, id int64) (*Instance, error) {
	in, err := scanInst(s.db.QueryRowContext(ctx, `SELECT `+instCols+` FROM instances i WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return in, err
}

func (s *Store) InstanceByToken(ctx context.Context, tok string) (*Instance, error) {
	if tok == "" {
		return nil, ErrNotFound
	}
	in, err := scanInst(s.db.QueryRowContext(ctx, `SELECT `+instCols+` FROM instances i WHERE token=?`, tok))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return in, err
}

func (s *Store) InsertInstance(ctx context.Context, in *Instance) error {
	in.CreatedAt = time.Now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO instances(request_id,step_id,item,item_label,kind,title,state,assignee,token,due_at,created_at,done_at,done_by,done_by_name,comment,outputs,checks)
	  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, in.RequestID, in.StepID, in.Item, in.ItemLabel, in.Kind, in.Title, in.State, js(in.Assignee), in.Token, in.DueAt, in.CreatedAt,
		in.DoneAt, in.DoneBy, in.DoneByName, in.Comment, js(in.Outputs), js(in.Checks))
	if err != nil {
		return err
	}
	in.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateInstance(ctx context.Context, in *Instance) error {
	_, err := s.db.ExecContext(ctx, `UPDATE instances SET state=?, assignee=?, token=?, due_at=?, done_at=?, done_by=?, done_by_name=?, comment=?, outputs=?, checks=?, reminded=? WHERE id=?`,
		in.State, js(in.Assignee), in.Token, in.DueAt, in.DoneAt, in.DoneBy, in.DoneByName, in.Comment, js(in.Outputs), js(in.Checks), in.Reminded, in.ID)
	return err
}

// Inbox liefert offene Schritte, die einer Person (direkt oder über Stelle) zugewiesen sind. v=nil: alle offenen.
func (s *Store) Inbox(ctx context.Context, v *Visibility, limit int) ([]*Instance, error) {
	q := `SELECT ` + instCols + `, r.number, r.title, r.wf_name FROM instances i JOIN requests r ON r.id=i.request_id WHERE i.state IN ('offen','rueckfrage')`
	var args []any
	if v != nil {
		w, a := visClause(v)
		q += ` AND ` + strings.ReplaceAll(w, "assignee", "i.assignee")
		args = a
	}
	q += ` ORDER BY CASE WHEN i.due_at IS NULL THEN 1 ELSE 0 END, i.due_at, i.id LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Instance
	for rows.Next() {
		var in *Instance
		var num, title, wf string
		in, err = scanInst(rows, &num, &title, &wf)
		if err != nil {
			return nil, err
		}
		in.ReqNumber, in.ReqTitle, in.ReqWf = num, title, wf
		out = append(out, in)
	}
	return out, rows.Err()
}

// Due liefert geplante Schritte (Termin) und überfällige offene Schritte für den Hintergrundlauf.
// ExternInstances liefert Schritte, die auf ein Fremdsystem (GLPI) warten.
func (s *Store) ExternInstances(ctx context.Context) ([]*Instance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+instCols+` FROM instances i WHERE i.state='extern' ORDER BY i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Instance
	for rows.Next() {
		in, err := scanInst(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (s *Store) DueInstances(ctx context.Context) ([]*Instance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+instCols+` FROM instances i WHERE (i.state='geplant' AND i.due_at <= ?) OR (i.state='offen' AND i.reminded=0 AND i.due_at <= ?)`, time.Now(), time.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Instance
	for rows.Next() {
		in, err := scanInst(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// ---------- Journal & Mails ----------

type JournalEntry struct {
	ID         int64
	RequestID  int64
	InstanceID int64
	At         time.Time
	Actor      string
	ActorName  string
	Kind       string
	Msg        string
	Detail     string
	ReqNumber  string
	ReqTitle   string
}

func (s *Store) Journal(ctx context.Context, e JournalEntry) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO journal(request_id,instance_id,at,actor,actor_name,kind,msg,detail) VALUES(?,?,?,?,?,?,?,?)`,
		e.RequestID, e.InstanceID, e.At, e.Actor, e.ActorName, e.Kind, e.Msg, e.Detail)
}

func (s *Store) JournalFor(ctx context.Context, reqID int64) ([]JournalEntry, error) {
	return s.journalQuery(ctx, `WHERE j.request_id=? ORDER BY j.id`, reqID)
}

func (s *Store) RecentJournal(ctx context.Context, v *Visibility, limit int) ([]JournalEntry, error) {
	if v == nil {
		return s.journalQuery(ctx, `ORDER BY j.id DESC LIMIT ?`, limit)
	}
	w, a := visClause(v)
	args := append([]any{v.Username}, a...)
	return s.journalQuery(ctx, `WHERE (r.requester=? OR j.request_id IN (SELECT request_id FROM instances WHERE `+w+`)) ORDER BY j.id DESC LIMIT ?`, append(args, limit)...)
}

func (s *Store) journalQuery(ctx context.Context, tail string, args ...any) ([]JournalEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT j.id, j.request_id, j.instance_id, j.at, j.actor, j.actor_name, j.kind, j.msg, j.detail, COALESCE(r.number,''), COALESCE(r.title,'')
	  FROM journal j LEFT JOIN requests r ON r.id=j.request_id `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JournalEntry
	for rows.Next() {
		var e JournalEntry
		if err := rows.Scan(&e.ID, &e.RequestID, &e.InstanceID, &e.At, &e.Actor, &e.ActorName, &e.Kind, &e.Msg, &e.Detail, &e.ReqNumber, &e.ReqTitle); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type Mail struct {
	ID         int64
	RequestID  int64
	InstanceID int64
	At         time.Time
	To         string
	Subject    string
	Body       string
	Link       string
	Status     string // gesendet, fehler, protokolliert
	Error      string
	ReqNumber  string
}

func (s *Store) InsertMail(ctx context.Context, m *Mail) error {
	m.At = time.Now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO mails(request_id,instance_id,at,to_addr,subject,body,link,status,error) VALUES(?,?,?,?,?,?,?,?,?)`,
		m.RequestID, m.InstanceID, m.At, m.To, m.Subject, m.Body, m.Link, m.Status, m.Error)
	if err == nil {
		m.ID, _ = res.LastInsertId()
	}
	return err
}

func (s *Store) Mails(ctx context.Context, reqID int64, limit int) ([]Mail, error) {
	q := `SELECT m.id, m.request_id, m.instance_id, m.at, m.to_addr, m.subject, m.body, m.link, m.status, m.error, COALESCE(r.number,'') FROM mails m LEFT JOIN requests r ON r.id=m.request_id`
	var args []any
	if reqID > 0 {
		q += ` WHERE m.request_id=?`
		args = append(args, reqID)
	}
	q += ` ORDER BY m.id DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mail
	for rows.Next() {
		var m Mail
		if err := rows.Scan(&m.ID, &m.RequestID, &m.InstanceID, &m.At, &m.To, &m.Subject, &m.Body, &m.Link, &m.Status, &m.Error, &m.ReqNumber); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Mail(ctx context.Context, id int64) (*Mail, error) {
	var m Mail
	err := s.db.QueryRowContext(ctx, `SELECT m.id, m.request_id, m.instance_id, m.at, m.to_addr, m.subject, m.body, m.link, m.status, m.error, COALESCE(r.number,'') FROM mails m LEFT JOIN requests r ON r.id=m.request_id WHERE m.id=?`, id).
		Scan(&m.ID, &m.RequestID, &m.InstanceID, &m.At, &m.To, &m.Subject, &m.Body, &m.Link, &m.Status, &m.Error, &m.ReqNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &m, err
}

// Tx führt fn exklusiv aus (Serialisierung der Ablaufsteuerung).
func (s *Store) DB() *sql.DB { return s.db }

func containsStr(l []string, x string) bool {
	for _, y := range l {
		if y == x {
			return true
		}
	}
	return false
}

// StatRow ist ein Vorgang für Kennzahlen.
type StatRow struct {
	WfName    string
	WfKey     string
	Status    string
	CreatedAt time.Time
	ClosedAt  sql.NullTime
	ID        int64
	Number    string
	Title     string
}

// StatRequests liefert alle offenen und die seit since erstellten/geschlossenen Vorgänge.
func (s *Store) StatRequests(ctx context.Context, since time.Time) ([]StatRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, number, title, wf_name, wf_key, status, created_at, closed_at FROM requests WHERE status IN ('laufend','rueckfrage') OR created_at >= ? OR closed_at >= ?`, since, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatRow
	for rows.Next() {
		var r StatRow
		if err := rows.Scan(&r.ID, &r.Number, &r.Title, &r.WfName, &r.WfKey, &r.Status, &r.CreatedAt, &r.ClosedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StatStep ist ein Schritt für Kennzahlen.
type StatStep struct {
	Kind      string
	State     string
	Assignee  string
	CreatedAt time.Time
	DoneAt    sql.NullTime
	DueAt     sql.NullTime
}

// StatSteps liefert offene Schritte und seit since erledigte Schritte (ohne übersprungene).
func (s *Store) StatSteps(ctx context.Context, since time.Time) ([]StatStep, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, state, assignee, created_at, done_at, due_at FROM instances
	  WHERE state IN ('offen','rueckfrage','extern') OR (state IN ('erledigt','freigegeben','abgelehnt') AND done_at >= ?)`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatStep
	for rows.Next() {
		var x StatStep
		var as string
		if err := rows.Scan(&x.Kind, &x.State, &as, &x.CreatedAt, &x.DoneAt, &x.DueAt); err != nil {
			return nil, err
		}
		var a struct {
			Label string `json:"label"`
		}
		_ = json.Unmarshal([]byte(as), &a)
		x.Assignee = a.Label
		out = append(out, x)
	}
	return out, rows.Err()
}
