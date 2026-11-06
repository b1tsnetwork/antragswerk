package ui

// TextDefs sind alle anpassbaren Texte mit Vorgabe. Reihenfolge = Anzeige unter Einstellungen → Texte.
// Texte mit Tpl=true sind Go-Vorlagen (z. B. {{.antrag.nummer}}); **fett** ist in Hinweistexten möglich.
var TextDefs = []TextDef{
	// Allgemein
	{Key: "app.tagline", Group: "Allgemein", Label: "Überschrift Anmeldeseite", Default: "Anträge stellen, freigeben, umsetzen – nachvollziehbar."},
	{Key: "login.usp1", Group: "Allgemein", Label: "Anmeldeseite Punkt 1", Default: "**Ein Formular** für Eintritt, Wechsel, Änderung und Austritt"},
	{Key: "login.usp2", Group: "Allgemein", Label: "Anmeldeseite Punkt 2", Default: "**Freigaben** durch Vorgesetzte, Verfahrensverantwortliche und Dateneigentümer – parallel"},
	{Key: "login.usp3", Group: "Allgemein", Label: "Anmeldeseite Punkt 3", Default: "**Fertige PowerShell-Skripte** mit den Antragsdaten für jeden Arbeitsschritt"},
	{Key: "login.usp4", Group: "Allgemein", Label: "Anmeldeseite Punkt 4", Default: "**Lückenloser Nachweis** jedes Schritts"},
	{Key: "login.title", Group: "Allgemein", Label: "Anmeldung – Überschrift", Default: "Anmelden"},
	{Key: "login.user", Group: "Allgemein", Label: "Anmeldung – Benutzername", Default: "Benutzername"},
	{Key: "login.pass", Group: "Allgemein", Label: "Anmeldung – Kennwort", Default: "Kennwort"},
	{Key: "login.button", Group: "Allgemein", Label: "Anmeldung – Schaltfläche", Default: "Anmelden"},
	{Key: "login.hint", Group: "Allgemein", Label: "Anmeldung – Hinweis", Default: "Anmeldung mit Ihrem Windows-Konto."},
	{Key: "footer.text", Group: "Allgemein", Label: "Fußzeile (Zusatz)", Default: ""},
	{Key: "demo.bar", Group: "Allgemein", Label: "Hinweisleiste im Demo-Modus", Default: "Demo-Modus · Mails werden nur im Postausgang protokolliert · Benutzer wechseln über „Abmelden“"},

	// Navigation
	{Key: "nav.home", Group: "Navigation", Label: "Startseite", Default: "Übersicht"},
	{Key: "nav.neu", Group: "Navigation", Label: "Antrag stellen", Default: "Antrag stellen"},
	{Key: "nav.inbox", Group: "Navigation", Label: "Meine Aufgaben", Default: "Meine Aufgaben"},
	{Key: "nav.list", Group: "Navigation", Label: "Vorgänge", Default: "Vorgänge"},
	{Key: "nav.designer", Group: "Navigation", Label: "Designer", Default: "Designer"},
	{Key: "nav.master", Group: "Navigation", Label: "Stammdaten", Default: "Stammdaten"},
	{Key: "nav.settings", Group: "Navigation", Label: "Einstellungen", Default: "Einstellungen"},
	{Key: "nav.outbox", Group: "Navigation", Label: "Postausgang", Default: "Postausgang"},
	{Key: "nav.audit", Group: "Navigation", Label: "Protokoll", Default: "Protokoll"},
	{Key: "nav.more", Group: "Navigation", Label: "Weitere Menüpunkte", Default: "Mehr"},
	{Key: "nav.logout", Group: "Navigation", Label: "Abmelden", Default: "Abmelden"},
	{Key: "nav.appearance", Group: "Navigation", Label: "Benutzermenü – Darstellung", Default: "Darstellung"},

	// Startseite
	{Key: "home.greeting", Group: "Startseite", Label: "Begrüßung (%s = Name)", Default: "Guten Tag, %s"},
	{Key: "home.none", Group: "Startseite", Label: "Keine Aufgaben", Default: "Keine offenen Aufgaben – alles erledigt."},
	{Key: "home.some", Group: "Startseite", Label: "Offene Aufgaben (%d = Anzahl)", Default: "Sie haben %d offene Aufgabe(n)."},
	{Key: "home.btn_new", Group: "Startseite", Label: "Schaltfläche neuer Antrag", Default: "+ Antrag stellen"},
	{Key: "home.tasks", Group: "Startseite", Label: "Kasten Meine Aufgaben", Default: "Meine Aufgaben"},
	{Key: "home.tasks_empty", Group: "Startseite", Label: "Keine Aufgaben (Kasten)", Default: "Nichts zu tun. Neue Aufgaben erscheinen hier und kommen per Mail."},
	{Key: "home.mine", Group: "Startseite", Label: "Kasten Meine Anträge", Default: "Meine Anträge"},
	{Key: "home.mine_empty", Group: "Startseite", Label: "Keine eigenen Anträge", Default: "Noch keine Anträge gestellt."},
	{Key: "home.quick", Group: "Startseite", Label: "Kasten Schnellstart", Default: "Antrag stellen"},
	{Key: "home.activity", Group: "Startseite", Label: "Kasten Aktivität", Default: "Aktivität"},
	{Key: "home.stat_tasks", Group: "Startseite", Label: "Kennzahl Aufgaben", Default: "Meine Aufgaben"},
	{Key: "home.stat_mine", Group: "Startseite", Label: "Kennzahl eigene Anträge", Default: "Eigene Anträge"},
	{Key: "home.stat_question", Group: "Startseite", Label: "Kennzahl Rückfrage", Default: "mit Rückfrage"},
	{Key: "home.stat_running", Group: "Startseite", Label: "Kennzahl in Bearbeitung", Default: "in Bearbeitung"},
	{Key: "home.stat_done", Group: "Startseite", Label: "Kennzahl abgeschlossen", Default: "abgeschlossen"},
	{Key: "portal.title", Group: "Startseite", Label: "Antragsportal – Überschrift", Default: "Was möchten Sie beantragen?"},
	{Key: "portal.lead", Group: "Startseite", Label: "Antragsportal – Einleitung", Default: "Wählen Sie den passenden Antrag. Freigaben, Aufgaben und Benachrichtigungen laufen danach automatisch."},
	{Key: "portal.status", Group: "Startseite", Label: "Antragsportal – Kasten eigene Anträge", Default: "Stand Ihrer Anträge"},
	{Key: "work.title", Group: "Startseite", Label: "Arbeitskorb – Überschrift", Default: "Arbeitskorb"},
	{Key: "work.lead", Group: "Startseite", Label: "Arbeitskorb – Einleitung", Default: "Offene Freigaben und Aufgaben Ihrer Stellen – Überfälliges zuerst."},
	{Key: "kanban.title", Group: "Startseite", Label: "Kanban – Überschrift", Default: "Vorgänge nach Phase"},
	{Key: "kanban.freigabe", Group: "Startseite", Label: "Kanban – Spalte Freigabe", Default: "Freigabe"},
	{Key: "kanban.umsetzung", Group: "Startseite", Label: "Kanban – Spalte Umsetzung", Default: "Umsetzung"},
	{Key: "kanban.termin", Group: "Startseite", Label: "Kanban – Spalte Termin", Default: "Wartet auf Termin"},
	{Key: "kanban.rueckfrage", Group: "Startseite", Label: "Kanban – Spalte Rückfrage", Default: "Rückfrage"},
	{Key: "kanban.erledigt", Group: "Startseite", Label: "Kanban – Spalte erledigt", Default: "Erledigt (14 Tage)"},
	{Key: "leit.title", Group: "Startseite", Label: "Leitstand – Überschrift", Default: "Leitstand"},
	{Key: "leit.lead", Group: "Startseite", Label: "Leitstand – Einleitung", Default: "Wie gut laufen die Anträge? Durchlaufzeiten, Engpässe und Fristen der letzten 90 Tage."},

	// Antrag stellen & Formular
	{Key: "neu.title", Group: "Antrag stellen", Label: "Überschrift", Default: "Antrag stellen"},
	{Key: "neu.lead", Group: "Antrag stellen", Label: "Einleitung", Default: "Wählen Sie den passenden Antrag. Alle Schritte, Freigaben und Benachrichtigungen laufen danach automatisch."},
	{Key: "neu.search", Group: "Antrag stellen", Label: "Suchfeld", Default: "Suchen …"},
	{Key: "neu.none", Group: "Antrag stellen", Label: "Keine Anträge", Default: "Es sind noch keine Anträge veröffentlicht."},
	{Key: "form.submit", Group: "Antrag stellen", Label: "Schaltfläche Absenden", Default: "Antrag stellen"},
	{Key: "form.answer_submit", Group: "Antrag stellen", Label: "Schaltfläche Rückfrage beantworten", Default: "Rückfrage beantworten"},
	{Key: "form.progress", Group: "Antrag stellen", Label: "Fortschritt", Default: "Pflichtangaben"},
	{Key: "form.preview", Group: "Antrag stellen", Label: "Vorschau – Überschrift", Default: "Was passiert danach?"},
	{Key: "form.preview_lead", Group: "Antrag stellen", Label: "Vorschau – Einleitung", Default: "Freigaben und Aufgaben anhand Ihrer Angaben:"},
	{Key: "form.parallel", Group: "Antrag stellen", Label: "Vorschau – parallel", Default: "parallel"},
	{Key: "form.discard", Group: "Antrag stellen", Label: "Eingaben verwerfen", Default: "Eingaben verwerfen"},
	{Key: "form.draft_hint", Group: "Antrag stellen", Label: "Hinweis Entwurf", Default: "Ihre Eingaben werden automatisch als Entwurf in diesem Browser gespeichert."},
	{Key: "form.choose", Group: "Antrag stellen", Label: "Auswahl leer", Default: "– bitte wählen –"},
	{Key: "form.add", Group: "Antrag stellen", Label: "Eintrag hinzufügen (Vorgabe)", Default: "Eintrag hinzufügen"},
	{Key: "form.empty_group", Group: "Antrag stellen", Label: "Gruppe ohne Einträge", Default: "Noch keine Einträge."},
	{Key: "form.user_search", Group: "Antrag stellen", Label: "Personensuche", Default: "Name, Kennung oder Mail suchen …"},
	{Key: "form.me", Group: "Antrag stellen", Label: "Mich selbst eintragen", Default: "mich selbst eintragen"},
	{Key: "form.required", Group: "Antrag stellen", Label: "Fehler Pflichtfeld", Default: "Pflichtfeld"},
	{Key: "form.check", Group: "Antrag stellen", Label: "Fehler beim Absenden", Default: "Bitte Eingaben prüfen."},
	{Key: "form.sending", Group: "Antrag stellen", Label: "Wird gesendet", Default: "Wird gesendet …"},
	{Key: "form.question", Group: "Antrag stellen", Label: "Rückfrage – Überschrift", Default: "Rückfrage"},
	{Key: "form.question_hint", Group: "Antrag stellen", Label: "Rückfrage – Hinweis", Default: "Bitte ergänzen oder korrigieren Sie die Angaben und antworten Sie rechts."},
	{Key: "form.answer", Group: "Antrag stellen", Label: "Rückfrage – Antwortfeld", Default: "Antwort"},
	{Key: "disabled.title", Group: "Antrag stellen", Label: "Deaktivierter Antrag (%s = Name)", Default: "„%s“ ist derzeit deaktiviert"},
	{Key: "disabled.text", Group: "Antrag stellen", Label: "Deaktivierter Antrag – Text", Default: "Dieser Antrag kann im Moment nicht gestellt werden. Bereits gestellte Anträge werden weiter bearbeitet."},

	// Vorgang & Schritte
	{Key: "detail.flow", Group: "Vorgang", Label: "Kasten Ablauf", Default: "Ablauf"},
	{Key: "detail.pick", Group: "Vorgang", Label: "Schritt auswählen – Überschrift", Default: "Schritt auswählen"},
	{Key: "detail.pick_text", Group: "Vorgang", Label: "Schritt auswählen – Text", Default: "Klicken Sie im Ablauf auf einen Schritt, um Anleitung, Skripte, Checkliste und Bearbeitung zu sehen."},
	{Key: "detail.tab_data", Group: "Vorgang", Label: "Reiter Antrag", Default: "Antrag"},
	{Key: "detail.tab_log", Group: "Vorgang", Label: "Reiter Verlauf", Default: "Verlauf"},
	{Key: "detail.tab_mail", Group: "Vorgang", Label: "Reiter Mails", Default: "Mails"},
	{Key: "detail.tab_values", Group: "Vorgang", Label: "Reiter Werte", Default: "Werte"},
	{Key: "detail.print", Group: "Vorgang", Label: "Schaltfläche Nachweis", Default: "🖨 Nachweis"},
	{Key: "detail.note", Group: "Vorgang", Label: "Schaltfläche Notiz", Default: "Notiz"},
	{Key: "detail.withdraw", Group: "Vorgang", Label: "Schaltfläche Zurückziehen", Default: "Zurückziehen"},
	{Key: "detail.question", Group: "Vorgang", Label: "Rückfrage-Hinweis", Default: "Rückfrage zu Ihrem Antrag"},
	{Key: "detail.question_btn", Group: "Vorgang", Label: "Rückfrage-Schaltfläche", Default: "Antrag ergänzen →"},
	{Key: "detail.mine", Group: "Vorgang", Label: "Markierung eigene Aufgabe", Default: "Ihre Aufgabe"},
	{Key: "detail.planned", Group: "Vorgang", Label: "Ausstehender Schritt – Text", Default: "Dieser Schritt startet automatisch, sobald alle vorherigen Schritte abgeschlossen sind. Die Zuständigen werden dann per Mail informiert."},
	{Key: "step.why", Group: "Vorgang", Label: "Schritt – Warum?", Default: "Warum?"},
	{Key: "step.checklist", Group: "Vorgang", Label: "Schritt – Checkliste", Default: "Checkliste"},
	{Key: "step.scripts", Group: "Vorgang", Label: "Schritt – Skripte", Default: "Skripte"},
	{Key: "step.scripts_hint", Group: "Vorgang", Label: "Schritt – Skripte Hinweis", Default: "mit den Antragsdaten – prüfen, kopieren, ausführen"},
	{Key: "step.copy", Group: "Vorgang", Label: "Schritt – Kopieren", Default: "Kopieren"},
	{Key: "step.result", Group: "Vorgang", Label: "Schritt – Ergebnis", Default: "Ergebnis"},
	{Key: "step.comment", Group: "Vorgang", Label: "Schritt – Kommentar", Default: "Kommentar"},
	{Key: "step.approve", Group: "Vorgang", Label: "Schaltfläche Freigeben", Default: "✔ Freigeben"},
	{Key: "step.reject", Group: "Vorgang", Label: "Schaltfläche Ablehnen", Default: "✕ Ablehnen"},
	{Key: "step.done", Group: "Vorgang", Label: "Schaltfläche Erledigt", Default: "✔ Erledigt"},
	{Key: "step.ask", Group: "Vorgang", Label: "Schaltfläche Rückfrage", Default: "? Rückfrage an Antragsteller/in"},
	{Key: "step.reassign", Group: "Vorgang", Label: "Weiterleiten", Default: "Weiterleiten / Vertretung …"},
	{Key: "step.reject_confirm", Group: "Vorgang", Label: "Sicherheitsabfrage Ablehnen", Default: "Antrag wirklich ablehnen? Der gesamte Vorgang wird beendet."},
	{Key: "token.intro", Group: "Vorgang", Label: "Mail-Link-Seite – Hinweis", Default: "Sie bearbeiten diesen Schritt über einen persönlichen Mail-Link. Ihre Entscheidung wird mit Zeitpunkt dokumentiert."},
	{Key: "token.invalid", Group: "Vorgang", Label: "Mail-Link ungültig", Default: "Dieser Link ist nicht mehr gültig – der Schritt wurde bereits bearbeitet oder weitergeleitet."},

	// Status
	{Key: "status.laufend", Group: "Status & Begriffe", Label: "Status In Bearbeitung", Default: "In Bearbeitung"},
	{Key: "status.rueckfrage", Group: "Status & Begriffe", Label: "Status Rückfrage", Default: "Rückfrage"},
	{Key: "status.abgeschlossen", Group: "Status & Begriffe", Label: "Status Abgeschlossen", Default: "Abgeschlossen"},
	{Key: "status.abgelehnt", Group: "Status & Begriffe", Label: "Status Abgelehnt", Default: "Abgelehnt"},
	{Key: "status.zurueckgezogen", Group: "Status & Begriffe", Label: "Status Zurückgezogen", Default: "Zurückgezogen"},
	{Key: "state.offen", Group: "Status & Begriffe", Label: "Schritt offen", Default: "Offen"},
	{Key: "state.erledigt", Group: "Status & Begriffe", Label: "Schritt erledigt", Default: "Erledigt"},
	{Key: "state.freigegeben", Group: "Status & Begriffe", Label: "Schritt freigegeben", Default: "Freigegeben"},
	{Key: "state.abgelehnt", Group: "Status & Begriffe", Label: "Schritt abgelehnt", Default: "Abgelehnt"},
	{Key: "state.rueckfrage", Group: "Status & Begriffe", Label: "Schritt Rückfrage", Default: "Rückfrage"},
	{Key: "state.uebersprungen", Group: "Status & Begriffe", Label: "Schritt entfällt", Default: "Entfällt"},
	{Key: "state.abgebrochen", Group: "Status & Begriffe", Label: "Schritt abgebrochen", Default: "Abgebrochen"},
	{Key: "state.geplant", Group: "Status & Begriffe", Label: "Schritt wartet", Default: "Wartet auf Termin"},
	{Key: "state.extern", Group: "Status & Begriffe", Label: "Schritt wartet auf GLPI", Default: "Bei GLPI"},
	{Key: "state.ausstehend", Group: "Status & Begriffe", Label: "Schritt ausstehend", Default: "Ausstehend"},
	{Key: "kind.freigabe", Group: "Status & Begriffe", Label: "Schrittart Freigabe", Default: "Freigabe"},
	{Key: "kind.aufgabe", Group: "Status & Begriffe", Label: "Schrittart Aufgabe", Default: "Aufgabe"},
	{Key: "kind.info", Group: "Status & Begriffe", Label: "Schrittart Benachrichtigung", Default: "Benachrichtigung"},
	{Key: "kind.warten", Group: "Status & Begriffe", Label: "Schrittart Termin", Default: "Termin abwarten"},
	{Key: "kind.glpi", Group: "Status & Begriffe", Label: "Schrittart GLPI", Default: "GLPI-Ticket"},

	// Meldungen
	{Key: "flash.gestellt", Group: "Meldungen", Label: "Antrag gestellt", Default: "Ihr Antrag wurde gestellt. Die zuständigen Stellen sind informiert."},
	{Key: "flash.erledigt", Group: "Meldungen", Label: "Schritt erledigt", Default: "Schritt erledigt – nachfolgende Schritte wurden gestartet."},
	{Key: "flash.freigegeben", Group: "Meldungen", Label: "Freigabe erteilt", Default: "Freigabe erteilt."},
	{Key: "flash.abgelehnt", Group: "Meldungen", Label: "Abgelehnt", Default: "Antrag abgelehnt – die antragstellende Person wurde informiert."},
	{Key: "flash.rueckfrage", Group: "Meldungen", Label: "Rückfrage gestellt", Default: "Rückfrage gestellt – der Antrag wartet auf Ergänzung."},
	{Key: "flash.beantwortet", Group: "Meldungen", Label: "Rückfrage beantwortet", Default: "Rückfrage beantwortet – der Ablauf geht weiter."},
	{Key: "flash.token_done", Group: "Meldungen", Label: "Mail-Link: erledigt", Default: "Vielen Dank – der Schritt ist als erledigt dokumentiert."},
	{Key: "flash.token_approved", Group: "Meldungen", Label: "Mail-Link: freigegeben", Default: "Vielen Dank – Ihre Freigabe ist dokumentiert."},
	{Key: "flash.token_rejected", Group: "Meldungen", Label: "Mail-Link: abgelehnt", Default: "Die Ablehnung ist dokumentiert. Die antragstellende Person wurde informiert."},
	{Key: "flash.token_question", Group: "Meldungen", Label: "Mail-Link: Rückfrage", Default: "Ihre Rückfrage wurde an die antragstellende Person gesendet."},

	// Mails (Vorlagen)
	{Key: "mail.aufgabe.betreff", Group: "Mails", Tpl: true, Label: "Aufgabe – Betreff", Default: "[{{.antrag.nummer}}] Aufgabe: {{.schritt.titel}}{{if .schritt.eintrag}} · {{.schritt.eintrag}}{{end}} – {{.antrag.titel}}"},
	{Key: "mail.aufgabe.text", Group: "Mails", Tpl: true, Label: "Aufgabe – Text", Default: `Guten Tag,

im Vorgang {{.antrag.nummer}} „{{.antrag.titel}}“ ist folgender Arbeitsschritt für Sie ({{.schritt.zustaendig}}) bereit:

  ▢ {{.schritt.titel}}{{if .schritt.eintrag}} · {{.schritt.eintrag}}{{end}}
{{- if .schritt.frist}}
  Frist: {{.schritt.frist}}
{{- end}}
{{- if .schritt.warum}}

Warum: {{.schritt.warum}}
{{- end}}
{{- if .schritt.anleitung}}

{{.schritt.anleitung}}
{{- end}}
{{- if .schritt.checkliste}}

Zu erledigen:
{{- range .schritt.checkliste}}
  ☐ {{.}}
{{- end}}
{{- end}}
{{- if .schritt.skripte}}

Passende Skripte mit den Antragsdaten stehen im Schritt zum Kopieren bereit:
{{- range .schritt.skripte}}
  • {{.}}
{{- end}}
{{- end}}

Zum Arbeitsschritt (Skripte, Checkliste, Bestätigung):
{{.link}}

Beantragt von {{.antrag.antragsteller_name}} am {{.antrag.datum}}.`},
	{Key: "mail.freigabe.betreff", Group: "Mails", Tpl: true, Label: "Freigabe – Betreff", Default: "[{{.antrag.nummer}}] Freigabe erbeten: {{.antrag.titel}}"},
	{Key: "mail.freigabe.text", Group: "Mails", Tpl: true, Label: "Freigabe – Text", Default: `Guten Tag,

für den Antrag {{.antrag.nummer}} „{{.antrag.titel}}“ wird Ihre Freigabe benötigt:

  ✔ {{.schritt.titel}}{{if .schritt.eintrag}} · {{.schritt.eintrag}}{{end}}
{{- if .schritt.frist}}
  Frist: {{.schritt.frist}}
{{- end}}
{{- if .schritt.warum}}

Warum: {{.schritt.warum}}
{{- end}}
{{- if .schritt.anleitung}}

{{.schritt.anleitung}}
{{- end}}

Angaben im Antrag:
{{.zusammenfassung}}

Freigeben, ablehnen oder Rückfrage stellen:
{{.link}}

Beantragt von {{.antrag.antragsteller_name}} am {{.antrag.datum}}.`},
	{Key: "mail.info.betreff", Group: "Mails", Tpl: true, Label: "Benachrichtigung – Betreff", Default: "[{{.antrag.nummer}}] Information: {{.antrag.titel}}"},
	{Key: "mail.info.text", Group: "Mails", Tpl: true, Label: "Benachrichtigung – Text", Default: `Guten Tag,

zum Antrag {{.antrag.nummer}} „{{.antrag.titel}}“ erhalten Sie folgende Information:

  {{.schritt.titel}}{{if .schritt.eintrag}} · {{.schritt.eintrag}}{{end}}
{{- if .schritt.anleitung}}

{{.schritt.anleitung}}
{{- end}}

Beantragt von {{.antrag.antragsteller_name}} am {{.antrag.datum}}.`},
	{Key: "mail.erinnerung.betreff", Group: "Mails", Tpl: true, Label: "Erinnerung – Betreff", Default: "Erinnerung: [{{.antrag.nummer}}] {{.schritt.titel}} ist überfällig"},
	{Key: "mail.erinnerung.text", Group: "Mails", Tpl: true, Label: "Erinnerung – Text", Default: `Guten Tag,

der Schritt „{{.schritt.titel}}“ im Vorgang {{.antrag.nummer}} („{{.antrag.titel}}“) ist seit {{.schritt.frist}} überfällig.

Bitte bearbeiten Sie ihn zeitnah oder leiten Sie ihn an die Vertretung weiter:
{{.link}}`},
	{Key: "mail.abgeschlossen.betreff", Group: "Mails", Tpl: true, Label: "Abschluss – Betreff", Default: "[{{.antrag.nummer}}] Erledigt: {{.antrag.titel}}"},
	{Key: "mail.abgeschlossen.text", Group: "Mails", Tpl: true, Label: "Abschluss – Text", Default: `Guten Tag,

Ihr Antrag {{.antrag.nummer}} „{{.antrag.titel}}“ ist vollständig bearbeitet.

Details und Verlauf:
{{.link}}`},
	{Key: "mail.abgelehnt.betreff", Group: "Mails", Tpl: true, Label: "Ablehnung – Betreff", Default: "[{{.antrag.nummer}}] Abgelehnt: {{.antrag.titel}}"},
	{Key: "mail.abgelehnt.text", Group: "Mails", Tpl: true, Label: "Ablehnung – Text", Default: `Guten Tag,

Ihr Antrag {{.antrag.nummer}} „{{.antrag.titel}}“ wurde abgelehnt.

Begründung:
{{.begruendung}}

Verlauf:
{{.link}}`},
	{Key: "mail.rueckfrage.betreff", Group: "Mails", Tpl: true, Label: "Rückfrage – Betreff", Default: "[{{.antrag.nummer}}] Rückfrage zu Ihrem Antrag: {{.antrag.titel}}"},
	{Key: "mail.rueckfrage.text", Group: "Mails", Tpl: true, Label: "Rückfrage – Text", Default: `Guten Tag,

zu Ihrem Antrag {{.antrag.nummer}} „{{.antrag.titel}}“ gibt es eine Rückfrage:

  {{.begruendung}}

Bitte ergänzen oder korrigieren Sie den Antrag hier:
{{.link}}`},
	{Key: "mail.button", Group: "Mails", Label: "HTML-Mail – Schaltfläche", Default: "Zum Arbeitsschritt öffnen"},
	{Key: "mail.footer", Group: "Mails", Label: "HTML-Mail – Fußzeile", Default: "Automatisch erzeugte Nachricht – bitte nicht antworten."},
}
