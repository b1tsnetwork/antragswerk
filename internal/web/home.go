package web

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"antragswerk/internal/store"
	"antragswerk/internal/ui"
)

type viewTab struct {
	Key, Label, Icon string
	On               bool
}

type homeData struct {
	View      string
	Views     []viewTab
	Welcome   string
	Inbox     []*store.Instance
	Mine      []*store.Request
	Recent    []store.JournalEntry
	Counts    map[string]int
	Workflows []*store.Workflow
	Groups    []*catGroup
	Overdue   int
	Staff     bool
	All       bool
	// Arbeitskorb
	Baskets []*basket
	// Kanban
	Columns []*kanbanCol
	// Leitstand
	Leit *leitData
}

type basket struct {
	Label   string
	Items   []*store.Instance
	Overdue int
}

type kanbanCol struct {
	Key   string
	Label string
	Items []*store.Request
	Tone  string
}

type kpi struct {
	Label, Value, Hint, Tone string
}

type barRow struct {
	Label string
	Value float64
	Text  string
	Pct   int
	Sub   string
	Tone  string
}

type weekBar struct {
	Label         string
	New, Closed   int
	NewP, ClosedP int
}

type leitData struct {
	KPIs      []kpi
	ByWf      []barRow
	Bottle    []barRow
	Speed     []barRow
	Weeks     []weekBar
	Oldest    []store.StatRow
	OldestAge []string
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	ctx := r.Context()
	wish := r.URL.Query().Get("ansicht")
	if wish == "" {
		if c, err := r.Cookie("aw_view"); err == nil {
			wish = c.Value
		}
	} else {
		http.SetCookie(w, &http.Cookie{Name: "aw_view", Value: wish, Path: "/", MaxAge: 365 * 86400, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.app.Cfg.CookieSecure})
	}
	view, allowed := ui.HomeView(se.Role, wish)
	d := homeData{View: view, Welcome: ui.Get().Home.Welcome, Staff: roleAtLeast(se.Role, "bearbeiter")}
	if len(allowed) > 1 {
		for _, k := range allowed {
			for _, v := range ui.Views {
				if v.Key == k {
					d.Views = append(d.Views, viewTab{Key: k, Label: v.Label, Icon: v.Icon, On: k == view})
				}
			}
		}
	}
	d.Inbox, _ = s.st.Inbox(ctx, s.visibility(se), 200)
	for _, in := range d.Inbox {
		if in.Overdue() {
			d.Overdue++
		}
	}
	d.Counts, _ = s.st.CountByStatus(ctx)
	wfs, _ := s.st.Workflows(ctx, false)
	sortWorkflows(wfs)
	idx := map[string]*catGroup{}
	for _, wf := range wfs {
		if wf.Active == nil || !s.mayUse(se, &wf.Active.Def) {
			continue
		}
		d.Workflows = append(d.Workflows, wf)
		c := wf.Active.Def.Category
		if c == "" {
			c = "Weitere Anträge"
		}
		g := idx[c]
		if g == nil {
			g = &catGroup{Name: c}
			idx[c] = g
			d.Groups = append(d.Groups, g)
		}
		g.Items = append(g.Items, wf)
	}
	switch view {
	case "standard":
		d.Mine, _, _ = s.st.Requests(ctx, store.RequestFilter{Requester: se.Username, Limit: 8})
		var vis *store.Visibility
		if !d.Staff {
			vis = s.visibility(se)
		}
		d.Recent, _ = s.st.RecentJournal(ctx, vis, 14)
		if len(d.Workflows) > 6 {
			d.Workflows = d.Workflows[:6]
		}
	case "kacheln":
		d.Mine, _, _ = s.st.Requests(ctx, store.RequestFilter{Requester: se.Username, Status: "", Limit: 6})
	case "aufgaben":
		items := d.Inbox
		if d.Staff && r.URL.Query().Get("alle") == "1" {
			d.All = true
			items, _ = s.st.Inbox(ctx, nil, 500)
		}
		d.Baskets = baskets(items)
	case "kanban":
		f := store.RequestFilter{Status: "offen", Limit: 300}
		if !d.Staff {
			f.VisibleTo = s.visibility(se)
		}
		open, _, _ := s.st.Requests(ctx, f)
		f.Status, f.Limit = "abgeschlossen", 40
		done, _, _ := s.st.Requests(ctx, f)
		d.Columns = kanban(open, done)
	case "leitstand":
		d.Leit = s.leitstand(r)
	}
	s.render(w, r, "dashboard", page{Title: ui.T("nav.home"), Nav: "home", Wide: view == "kanban" || view == "leitstand", Data: d})
}

func baskets(items []*store.Instance) []*basket {
	by := map[string]*basket{}
	var out []*basket
	for _, in := range items {
		l := in.Assignee.Label
		if l == "" {
			l = "–"
		}
		b := by[l]
		if b == nil {
			b = &basket{Label: l}
			by[l] = b
			out = append(out, b)
		}
		b.Items = append(b.Items, in)
		if in.Overdue() {
			b.Overdue++
		}
	}
	for _, b := range out {
		sort.SliceStable(b.Items, func(i, j int) bool {
			a, c := b.Items[i], b.Items[j]
			if a.Overdue() != c.Overdue() {
				return a.Overdue()
			}
			return a.CreatedAt.Before(c.CreatedAt)
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Overdue != out[j].Overdue {
			return out[i].Overdue > out[j].Overdue
		}
		return len(out[i].Items) > len(out[j].Items)
	})
	return out
}

func kanban(open, done []*store.Request) []*kanbanCol {
	cols := []*kanbanCol{{Key: "freigabe", Tone: "freigabe"}, {Key: "umsetzung", Tone: "aufgabe"}, {Key: "termin", Tone: "warten"}, {Key: "rueckfrage", Tone: "warn"}, {Key: "erledigt", Tone: "ok"}}
	by := map[string]*kanbanCol{}
	for _, c := range cols {
		c.Label = ui.T("kanban." + c.Key)
		by[c.Key] = c
	}
	for _, r := range open {
		by[r.Phase()].Items = append(by[r.Phase()].Items, r)
	}
	cut := time.Now().AddDate(0, 0, -14)
	for _, r := range done {
		if r.ClosedAt.Valid && r.ClosedAt.Time.After(cut) {
			by["erledigt"].Items = append(by["erledigt"].Items, r)
		}
	}
	return cols
}

func days(d time.Duration) float64 { return d.Hours() / 24 }

func fmtDays(x float64) string {
	switch {
	case x < 1:
		return fmt.Sprintf("%.0f h", x*24)
	case x < 10:
		return strings.Replace(fmt.Sprintf("%.1f Tage", x), ".", ",", 1)
	}
	return fmt.Sprintf("%.0f Tage", x)
}

func (s *Server) leitstand(r *http.Request) *leitData {
	ctx := r.Context()
	since := time.Now().AddDate(0, 0, -90)
	reqs, _ := s.st.StatRequests(ctx, since)
	steps, _ := s.st.StatSteps(ctx, since)
	L := &leitData{}
	open, newCount, closed90 := 0, 0, 0
	var durSum float64
	type wfAgg struct {
		open, done int
		dur        float64
	}
	wf := map[string]*wfAgg{}
	var oldest []store.StatRow
	for _, x := range reqs {
		a := wf[x.WfName]
		if a == nil {
			a = &wfAgg{}
			wf[x.WfName] = a
		}
		if x.Status == "laufend" || x.Status == "rueckfrage" {
			open++
			a.open++
			oldest = append(oldest, x)
		}
		if x.CreatedAt.After(time.Now().AddDate(0, 0, -30)) {
			newCount++
		}
		if x.Status == "abgeschlossen" && x.ClosedAt.Valid && x.ClosedAt.Time.After(since) {
			closed90++
			dd := days(x.ClosedAt.Time.Sub(x.CreatedAt))
			durSum += dd
			a.done++
			a.dur += dd
		}
	}
	overdue := 0
	type stAgg struct {
		open, overdue, done int
		age, dur            float64
	}
	sa := map[string]*stAgg{}
	for _, x := range steps {
		if x.Assignee == "" {
			continue
		}
		a := sa[x.Assignee]
		if a == nil {
			a = &stAgg{}
			sa[x.Assignee] = a
		}
		if x.State == "offen" || x.State == "rueckfrage" {
			a.open++
			if age := days(time.Since(x.CreatedAt)); age > a.age {
				a.age = age
			}
			if x.State == "offen" && x.DueAt.Valid && time.Now().After(x.DueAt.Time) {
				a.overdue++
				overdue++
			}
		} else if x.DoneAt.Valid {
			a.done++
			a.dur += days(x.DoneAt.Time.Sub(x.CreatedAt))
		}
	}
	avg := "–"
	if closed90 > 0 {
		avg = fmtDays(durSum / float64(closed90))
	}
	ovTone := "ok"
	if overdue > 0 {
		ovTone = "bad"
	}
	L.KPIs = []kpi{{"Offene Vorgänge", fmt.Sprint(open), "laufend und mit Rückfrage", ""},
		{"Überfällige Schritte", fmt.Sprint(overdue), "Frist überschritten", ovTone},
		{"Ø Durchlaufzeit", avg, "Antrag bis Abschluss, 90 Tage", ""},
		{"Abgeschlossen", fmt.Sprint(closed90), "in den letzten 90 Tagen", "ok"},
		{"Neue Anträge", fmt.Sprint(newCount), "in den letzten 30 Tagen", ""}}
	// je Ablauf
	maxOpen := 1
	for _, a := range wf {
		if a.open+a.done > maxOpen {
			maxOpen = a.open + a.done
		}
	}
	for name, a := range wf {
		if a.open+a.done == 0 {
			continue
		}
		sub := ""
		if a.done > 0 {
			sub = "Ø " + fmtDays(a.dur/float64(a.done))
		}
		L.ByWf = append(L.ByWf, barRow{Label: name, Value: float64(a.open), Text: fmt.Sprintf("%d offen · %d erledigt", a.open, a.done), Pct: (a.open + a.done) * 100 / maxOpen, Sub: sub})
	}
	sort.Slice(L.ByWf, func(i, j int) bool { return L.ByWf[i].Pct > L.ByWf[j].Pct })
	// Engpässe
	maxO := 1
	for _, a := range sa {
		if a.open > maxO {
			maxO = a.open
		}
	}
	for name, a := range sa {
		if a.open > 0 {
			tone := ""
			if a.overdue > 0 {
				tone = "bad"
			}
			L.Bottle = append(L.Bottle, barRow{Label: name, Value: float64(a.open), Text: fmt.Sprintf("%d offen%s", a.open, map[bool]string{true: fmt.Sprintf(" · %d überfällig", a.overdue), false: ""}[a.overdue > 0]),
				Pct: a.open * 100 / maxO, Sub: "ältester seit " + fmtDays(a.age), Tone: tone})
		}
		if a.done > 0 {
			L.Speed = append(L.Speed, barRow{Label: name, Value: a.dur / float64(a.done), Text: "Ø " + fmtDays(a.dur/float64(a.done)), Sub: fmt.Sprintf("%d Schritte", a.done)})
		}
	}
	sort.Slice(L.Bottle, func(i, j int) bool { return L.Bottle[i].Value > L.Bottle[j].Value })
	if len(L.Bottle) > 8 {
		L.Bottle = L.Bottle[:8]
	}
	sort.Slice(L.Speed, func(i, j int) bool { return L.Speed[i].Value > L.Speed[j].Value })
	if len(L.Speed) > 8 {
		L.Speed = L.Speed[:8]
	}
	maxS := 0.01
	for _, x := range L.Speed {
		if x.Value > maxS {
			maxS = x.Value
		}
	}
	for i := range L.Speed {
		L.Speed[i].Pct = int(L.Speed[i].Value * 100 / maxS)
	}
	// Wochen
	now := time.Now()
	start := now.AddDate(0, 0, -int(now.Weekday()+6)%7).Truncate(24 * time.Hour)
	weeks := make([]weekBar, 12)
	for i := range weeks {
		ws := start.AddDate(0, 0, -7*(11-i))
		_, wn := ws.ISOWeek()
		weeks[i].Label = fmt.Sprintf("KW %d", wn)
		we := ws.AddDate(0, 0, 7)
		for _, x := range reqs {
			if !x.CreatedAt.Before(ws) && x.CreatedAt.Before(we) {
				weeks[i].New++
			}
			if x.ClosedAt.Valid && !x.ClosedAt.Time.Before(ws) && x.ClosedAt.Time.Before(we) {
				weeks[i].Closed++
			}
		}
	}
	mx := 1
	for _, wk := range weeks {
		if wk.New > mx {
			mx = wk.New
		}
		if wk.Closed > mx {
			mx = wk.Closed
		}
	}
	for i := range weeks {
		weeks[i].NewP, weeks[i].ClosedP = weeks[i].New*100/mx, weeks[i].Closed*100/mx
	}
	L.Weeks = weeks
	sort.Slice(oldest, func(i, j int) bool { return oldest[i].CreatedAt.Before(oldest[j].CreatedAt) })
	if len(oldest) > 6 {
		oldest = oldest[:6]
	}
	L.Oldest = oldest
	for _, o := range oldest {
		L.OldestAge = append(L.OldestAge, fmtDays(days(time.Since(o.CreatedAt))))
	}
	return L
}
