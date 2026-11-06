package flow

import (
	"fmt"
	"strings"
)

// GLPIStep beschreibt die Übergabe an GLPI (moderne API v2): Ticket oder Change anlegen bzw. ergänzen.
// Alle Textfelder sind Vorlagen mit Zugriff auf Antrag, berechnete Werte, Ergebnisse früherer Schritte und
// {{.glpi.*}} (Rückmeldelink, Token, Referenz).
type GLPIStep struct {
	Conn   string `json:"conn,omitempty" yaml:"conn,omitempty"`     // Verbindung (leer = erste)
	Action string `json:"action,omitempty" yaml:"action,omitempty"` // create (Standard) | followup
	Type   string `json:"type,omitempty" yaml:"type,omitempty"`     // ticket (Standard) | change
	Ref    string `json:"ref,omitempty" yaml:"ref,omitempty"`       // followup: Schritt, dessen Ticket/Change ergänzt wird

	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Content     string `json:"content,omitempty" yaml:"content,omitempty"`
	TicketType  string `json:"ticket_type,omitempty" yaml:"ticket_type,omitempty"` // 1 Störung, 2 Anfrage
	Status      string `json:"status,omitempty" yaml:"status,omitempty"`
	Urgency     string `json:"urgency,omitempty" yaml:"urgency,omitempty"`
	Impact      string `json:"impact,omitempty" yaml:"impact,omitempty"`
	Priority    string `json:"priority,omitempty" yaml:"priority,omitempty"`
	Entity      string `json:"entity,omitempty" yaml:"entity,omitempty"`     // ID oder vollständiger Name
	Category    string `json:"category,omitempty" yaml:"category,omitempty"` // ITIL-Kategorie
	Location    string `json:"location,omitempty" yaml:"location,omitempty"`
	RequestType string `json:"request_type,omitempty" yaml:"request_type,omitempty"` // Anfragequelle (nur Ticket)
	DueDate     string `json:"due_date,omitempty" yaml:"due_date,omitempty"`         // Lösungsfrist (resolution_date)
	ExternalID  string `json:"external_id,omitempty" yaml:"external_id,omitempty"`   // Standard: Referenz des Schritts

	// Change: Analyse und Pläne (GLPI-Felder impactcontent, controlistcontent, rolloutplancontent, backoutplancontent, checklistcontent)
	ImpactContent  string `json:"impact_content,omitempty" yaml:"impact_content,omitempty"`
	ControlContent string `json:"control_content,omitempty" yaml:"control_content,omitempty"`
	RolloutPlan    string `json:"rollout_plan,omitempty" yaml:"rollout_plan,omitempty"`
	BackoutPlan    string `json:"backout_plan,omitempty" yaml:"backout_plan,omitempty"`
	ChecklistPlan  string `json:"checklist_plan,omitempty" yaml:"checklist_plan,omitempty"`

	Team       []GLPITeam      `json:"team,omitempty" yaml:"team,omitempty"`
	Tasks      []GLPITask      `json:"tasks,omitempty" yaml:"tasks,omitempty"`
	Validation *GLPIValidation `json:"validation,omitempty" yaml:"validation,omitempty"`
	Private    bool            `json:"private,omitempty" yaml:"private,omitempty"` // Folgeeintrag privat
	Extra      string          `json:"extra,omitempty" yaml:"extra,omitempty"`     // weitere Felder als JSON-Vorlage (überschreibt)

	Wait       string `json:"wait,omitempty" yaml:"wait,omitempty"`               // done (Standard): bis gelöst · none: nur übergeben
	DoneStatus []int  `json:"done_status,omitempty" yaml:"done_status,omitempty"` // leer = Vorgabe je Art
	FailStatus []int  `json:"fail_status,omitempty" yaml:"fail_status,omitempty"`
	OnFail     string `json:"on_fail,omitempty" yaml:"on_fail,omitempty"` // aufgabe (Standard) | ablehnen | weiter
}

// GLPITeam ist ein Teammitglied: Rolle requester|assigned|observer, Art User|Group|Supplier, Wert = IDs oder Namen (; getrennt, Vorlage).
type GLPITeam struct {
	Role  string `json:"role" yaml:"role"`
	Type  string `json:"type" yaml:"type"`
	Value string `json:"value" yaml:"value"`
}

// GLPITask ist eine Aufgabe im Ticket/Change.
type GLPITask struct {
	Content   string `json:"content" yaml:"content"`
	State     string `json:"state,omitempty" yaml:"state,omitempty"`       // 0 Information, 1 Zu erledigen (Standard), 2 Erledigt
	Duration  string `json:"duration,omitempty" yaml:"duration,omitempty"` // Minuten
	UserTech  string `json:"user_tech,omitempty" yaml:"user_tech,omitempty"`
	GroupTech string `json:"group_tech,omitempty" yaml:"group_tech,omitempty"`
	Category  string `json:"category,omitempty" yaml:"category,omitempty"` // Aufgabenkategorie
	Begin     string `json:"begin,omitempty" yaml:"begin,omitempty"`
	End       string `json:"end,omitempty" yaml:"end,omitempty"`
	Private   bool   `json:"private,omitempty" yaml:"private,omitempty"`
	When      *Cond  `json:"when,omitempty" yaml:"when,omitempty"`
}

// GLPIValidation fordert in GLPI eine Genehmigung an.
type GLPIValidation struct {
	ApproverType string `json:"approver_type" yaml:"approver_type"` // User | Group
	Approver     string `json:"approver" yaml:"approver"`
	Comment      string `json:"comment,omitempty" yaml:"comment,omitempty"`
}

// GLPIDefaultContent ist der Standardtext eines neuen Tickets.
const GLPIDefaultContent = `{{.antrag.titel}} ({{.antrag.nummer}})
Antragsteller/in: {{.antrag.antragsteller_name}}

{{.zusammenfassung}}

Vorgang im Antragswerk: {{.glpi.vorgang}}
Rückmeldung (erledigt melden, Angaben eintragen): {{.glpi.rueckmeldung}}`

// IsChange: Art ist Change.
func (g *GLPIStep) IsChange() bool { return strings.EqualFold(g.Type, "change") }

func validateGLPI(d *Def, s *Step, add func(level, ref, f string, a ...any)) {
	ref := "step:" + s.ID
	g := s.GLPI
	if g == nil {
		add("error", ref, "„%s“: GLPI-Einstellungen fehlen.", s.Title)
		return
	}
	switch g.Action {
	case "", "create":
		if strings.TrimSpace(g.Name) == "" {
			add("error", ref, "„%s“: Titel des GLPI-Tickets fehlt.", s.Title)
		}
		if g.Wait != "none" && !strings.Contains(g.Content+g.Extra, ".glpi.rueckmeldung") && !strings.Contains(g.Content+g.Extra, ".glpi.api") && g.Content != "" {
			add("warn", ref, "„%s“: Der Text enthält keinen Rückmeldelink ({{.glpi.rueckmeldung}}) – Rückmeldung dann nur über Status/Webhook.", s.Title)
		}
	case "followup":
		r := d.StepByID(g.Ref)
		if r == nil || r.Kind != "glpi" {
			add("error", ref, "„%s“: Folgeeintrag braucht einen vorherigen GLPI-Schritt.", s.Title)
		}
		if strings.TrimSpace(g.Content) == "" {
			add("error", ref, "„%s“: Text des Folgeeintrags fehlt.", s.Title)
		}
	default:
		add("error", ref, "„%s“: unbekannte GLPI-Aktion %q.", s.Title, g.Action)
	}
	for i, t := range g.Team {
		if t.Value == "" {
			add("warn", ref, "„%s“: Team-Eintrag %d ohne Wert.", s.Title, i+1)
		}
	}
	if s.Assign.Type == "" {
		add("warn", ref, "„%s“: ohne Zuständigkeit kann niemand übernehmen, wenn GLPI nicht erreichbar ist oder das Ticket scheitert.", s.Title)
	}
	for _, o := range s.Outputs {
		if o.Key == "" {
			add("error", ref, "„%s“: Rückmeldefeld ohne Schlüssel.", s.Title)
		}
	}
	_ = fmt.Sprint
}
