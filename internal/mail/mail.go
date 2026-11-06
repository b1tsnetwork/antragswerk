// Package mail versendet Benachrichtigungen per SMTP (Text + HTML).
package mail

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"antragswerk/internal/config"
)

type Mailer struct{ cfg config.SMTP }

func New(cfg config.SMTP) *Mailer { return &Mailer{cfg: cfg} }

func (m *Mailer) Enabled() bool { return m.cfg.Enabled() }

func qp(s string) string {
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}

// Send verschickt eine Mail an alle Empfänger.
func (m *Mailer) Send(to []string, subject, text, html string) error {
	if !m.Enabled() {
		return errors.New("SMTP nicht konfiguriert")
	}
	if len(to) == 0 {
		return errors.New("keine Empfänger")
	}
	bnd := make([]byte, 12)
	_, _ = rand.Read(bnd)
	boundary := "aw-" + base64.RawURLEncoding.EncodeToString(bnd)
	var b bytes.Buffer
	h := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	h("From", m.cfg.From)
	h("To", strings.Join(to, ", "))
	h("Subject", mime.QEncoding.Encode("utf-8", subject))
	h("Date", time.Now().Format(time.RFC1123Z))
	h("MIME-Version", "1.0")
	h("Auto-Submitted", "auto-generated")
	h("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	for _, part := range []struct{ ct, body string }{{"text/plain", text}, {"text/html", html}} {
		if part.body == "" {
			continue
		}
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n%s\r\n", boundary, part.ct, qp(part.body))
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)

	port := m.cfg.Port
	if port == 0 {
		port = 587
		if m.cfg.Security == "tls" {
			port = 465
		} else if m.cfg.Security == "none" {
			port = 25
		}
	}
	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(port))
	tc := &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: m.cfg.Insecure} //nolint:gosec
	var conn net.Conn
	var err error
	d := &net.Dialer{Timeout: 15 * time.Second}
	if m.cfg.Security == "tls" {
		conn, err = tls.DialWithDialer(d, "tcp", addr, tc)
	} else {
		conn, err = d.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if m.cfg.Security == "" || m.cfg.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tc); err != nil {
				return err
			}
		} else if m.cfg.Security == "starttls" {
			return errors.New("Server bietet kein STARTTLS an")
		}
	}
	if m.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			return err
		}
	}
	from := m.cfg.From
	if a, err := mailAddr(from); err == nil {
		from = a
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("%s: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(b.Bytes()); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func mailAddr(s string) (string, error) {
	if i := strings.LastIndex(s, "<"); i >= 0 {
		j := strings.LastIndex(s, ">")
		if j > i {
			return s[i+1 : j], nil
		}
	}
	if strings.Contains(s, "@") {
		return strings.TrimSpace(s), nil
	}
	return "", errors.New("ungültige Absenderadresse")
}
