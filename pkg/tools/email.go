package tools

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/smtp"
	"strings"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
)

// ReadEmailTool fetches recent emails and returns them as text.
type ReadEmailTool struct {
	config config.EmailConfig
}

func NewReadEmailTool(cfg config.EmailConfig) *ReadEmailTool {
	return &ReadEmailTool{config: cfg}
}

func (t *ReadEmailTool) Name() string {
	return "read_email"
}

func (t *ReadEmailTool) Description() string {
	return "Fetch and read recent emails. Returns the sender, subject, and body of the last N emails."
}

func (t *ReadEmailTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"count": map[string]interface{}{
				"type":        "integer",
				"description": "Number of recent emails to fetch (default: 5, max: 10)",
			},
			"unread_only": map[string]interface{}{
				"type":        "boolean",
				"description": "If true, only fetch unread emails (default: false)",
			},
		},
	}
}

func (t *ReadEmailTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if !t.config.Enabled {
		return ErrorResult("Email channel is not enabled in configuration.")
	}

	count := 5
	if c, ok := args["count"].(float64); ok {
		count = int(c)
	}
	if count > 10 {
		count = 10
	}

	unreadOnly := false
	if u, ok := args["unread_only"].(bool); ok {
		unreadOnly = u
	}

	// Connect to IMAP
	addr := fmt.Sprintf("%s:%d", t.config.IMAPServer, t.config.IMAPPort)
	var c *client.Client
	var err error

	if t.config.IMAPPort == 993 {
		c, err = client.DialTLS(addr, nil)
	} else {
		c, err = client.Dial(addr)
	}
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to connect to IMAP: %v", err))
	}
	defer c.Logout()

	if err := c.Login(t.config.IMAPUser, t.config.IMAPPassword); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to login to IMAP: %v", err))
	}

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to select INBOX: %v", err))
	}

	if mbox.Messages == 0 {
		return SilentResult("Inbox is empty.")
	}

	// Search criteria
	criteria := imap.NewSearchCriteria()
	if unreadOnly {
		criteria.WithoutFlags = []string{imap.SeenFlag}
	} else {
		// Fetch all (limited by range)
		// imap.SearchCriteria doesn't have "ALL" by default, empty means all?
		// Actually, we can just use sequence numbers if we want "recent"
	}

	var uids []uint32
	if unreadOnly {
		uids, err = c.Search(criteria)
		if err != nil {
			return ErrorResult(fmt.Sprintf("Failed to search emails: %v", err))
		}
	} else {
		// Just get the last N messages by sequence number
		from := uint32(1)
		if mbox.Messages > uint32(count) {
			from = mbox.Messages - uint32(count) + 1
		}
		to := mbox.Messages
		seqset := new(imap.SeqSet)
		seqset.AddRange(from, to)

		// We need to fetch UIDs for these sequence numbers or just fetch directly
		// Let's fetch directly by SeqNum
		// But the processing logic uses UIDs usually. Let's stick to UIDs for consistency if possible,
		// but fetching by sequence is easier for "last N".
		// Let's use Fetch directly with the seqset.
	}

	// Reuse uids logic if unreadOnly, otherwise construct seqset
	seqset := new(imap.SeqSet)
	if unreadOnly {
		if len(uids) == 0 {
			return SilentResult("No unread emails.")
		}
		if len(uids) > count {
			uids = uids[len(uids)-count:]
		}
		seqset.AddNum(uids...)
	} else {
		from := uint32(1)
		if mbox.Messages > uint32(count) {
			from = mbox.Messages - uint32(count) + 1
		}
		to := mbox.Messages
		seqset.AddRange(from, to)
	}

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope}

	messages := make(chan *imap.Message)
	done := make(chan error, 1)

	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d emails (showing last %d):\n\n", mbox.Messages, count)) // Approx count

	for msg := range messages {
		sb.WriteString("---\n")
		sender := "unknown"
		if len(msg.Envelope.From) > 0 {
			sender = fmt.Sprintf("%s@%s", msg.Envelope.From[0].MailboxName, msg.Envelope.From[0].HostName)
		}
		sb.WriteString(fmt.Sprintf("From: %s\n", sender))
		sb.WriteString(fmt.Sprintf("Subject: %s\n", msg.Envelope.Subject))
		sb.WriteString(fmt.Sprintf("Date: %s\n", msg.Envelope.Date))

		r := msg.GetBody(section)
		if r != nil {
			mr, err := mail.CreateReader(r)
			if err == nil {
				for {
					p, err := mr.NextPart()
					if err == io.EOF {
						break
					} else if err != nil {
						break
					}
					switch h := p.Header.(type) {
					case *mail.InlineHeader:
						contentType, _, _ := h.ContentType()
						if contentType == "text/plain" {
							b, _ := io.ReadAll(p.Body)
							sb.WriteString("\n")
							sb.WriteString(string(b))
						}
					}
				}
			}
		}
		sb.WriteString("\n")
	}

	if err := <-done; err != nil {
		return ErrorResult(fmt.Sprintf("Failed to fetch emails: %v", err))
	}

	return &ToolResult{
		ForLLM:  sb.String(),
		ForUser: "Read recent emails.",
	}
}

// SendEmailTool sends an email.
type SendEmailTool struct {
	config config.EmailConfig
}

func NewSendEmailTool(cfg config.EmailConfig) *SendEmailTool {
	return &SendEmailTool{config: cfg}
}

func (t *SendEmailTool) Name() string {
	return "send_email"
}

func (t *SendEmailTool) Description() string {
	return "Send an email to a specific address."
}

func (t *SendEmailTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"to": map[string]interface{}{
				"type":        "string",
				"description": "Recipient email address (e.g. user@example.com)",
			},
			"subject": map[string]interface{}{
				"type":        "string",
				"description": "Email subject",
			},
			"body": map[string]interface{}{
				"type":        "string",
				"description": "Email body content",
			},
		},
		"required": []string{"to", "subject", "body"},
	}
}

func (t *SendEmailTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if !t.config.Enabled {
		return ErrorResult("Email channel is not enabled in configuration.")
	}

	to, _ := args["to"].(string)
	subject, _ := args["subject"].(string)
	bodyContent, _ := args["body"].(string)

	if to == "" {
		return ErrorResult("Recipient (to) is required.")
	}

	addr := fmt.Sprintf("%s:%d", t.config.SMTPServer, t.config.SMTPPort)
	auth := smtp.PlainAuth("", t.config.SMTPUser, t.config.SMTPPassword, t.config.SMTPServer)

	// RFC 822 format
	msg := fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", to, subject, bodyContent)

	logger.InfoCF("email", "Sending email via tool", map[string]interface{}{"to": to, "subject": subject})

	err := t.sendMail(addr, auth, t.config.SMTPUser, []string{to}, []byte(msg))
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to send email: %v", err))
	}

	return &ToolResult{
		ForLLM:  fmt.Sprintf("Email sent successfully to %s", to),
		ForUser: fmt.Sprintf("Sent email to %s", to),
	}
}

func (t *SendEmailTool) sendMail(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
	// Handle TLS logic similar to EmailChannel
	if t.config.SMTPPort == 465 {
		// Direct TLS
		tlsConfig := &tls.Config{
			ServerName: t.config.SMTPServer,
		}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return err
		}
		defer conn.Close()

		c, err := smtp.NewClient(conn, t.config.SMTPServer)
		if err != nil {
			return err
		}
		defer c.Quit()

		if err = c.Auth(a); err != nil {
			return err
		}
		if err = c.Mail(from); err != nil {
			return err
		}
		for _, addr := range to {
			if err = c.Rcpt(addr); err != nil {
				return err
			}
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		_, err = w.Write(msg)
		if err != nil {
			return err
		}
		return w.Close()
	} else {
		// STARTTLS
		return smtp.SendMail(addr, a, from, to, msg)
	}
}
