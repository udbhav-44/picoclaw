package channels

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/smtp"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
)

type EmailChannel struct {
	*BaseChannel
	config      config.EmailConfig
	imapClient  *client.Client
	manualCheck chan bool
}

func NewEmailChannel(cfg config.EmailConfig, bus *bus.MessageBus) (*EmailChannel, error) {
	base := NewBaseChannel("email", cfg, bus, cfg.AllowFrom)
	return &EmailChannel{
		BaseChannel: base,
		config:      cfg,
		manualCheck: make(chan bool, 1),
	}, nil
}

func (c *EmailChannel) Start(ctx context.Context) error {
	logger.InfoC("email", "Starting Email channel polling...")

	c.setRunning(true)
	go c.pollLoop(ctx)

	return nil
}

func (c *EmailChannel) Stop(ctx context.Context) error {
	logger.InfoC("email", "Stopping Email channel...")
	c.setRunning(false)
	if c.imapClient != nil {
		c.imapClient.Logout()
	}
	return nil
}

func (c *EmailChannel) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(c.config.PollInterval) * time.Second)
	defer ticker.Stop()

	// Initial poll
	c.checkMail()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkMail()
		case <-c.manualCheck:
			c.checkMail()
		}
	}
}

func (c *EmailChannel) checkMail() {
	// User feedback for manual check visibility
	fmt.Println("📧 Checking for new emails...")

	// Reconnect if needed
	if c.imapClient == nil || c.imapClient.State() == imap.LogoutState {
		fmt.Printf("📧 Connecting to IMAP server %s:%d...\n", c.config.IMAPServer, c.config.IMAPPort)
		if err := c.connectIMAP(); err != nil {
			logger.ErrorCF("email", "Failed to connect to IMAP", map[string]interface{}{"error": err.Error()})
			fmt.Printf("❌ Failed to connect to IMAP: %v\n", err)
			return
		}
		fmt.Println("✅ Connected to IMAP")
	}

	// Select INBOX
	mbox, err := c.imapClient.Select("INBOX", false)
	if err != nil {
		logger.ErrorCF("email", "Failed to select INBOX", map[string]interface{}{"error": err.Error()})
		fmt.Printf("❌ Failed to select INBOX: %v\n", err)
		// Force reconnect next time
		c.imapClient.Logout()
		c.imapClient = nil
		return
	}

	if mbox.Messages == 0 {
		fmt.Println("📭 Inbox is empty.")
		return
	}

	// Search for unread messages
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	uids, err := c.imapClient.Search(criteria)
	if err != nil {
		logger.ErrorCF("email", "Failed to search emails", map[string]interface{}{"error": err.Error()})
		fmt.Printf("❌ Failed to search emails: %v\n", err)
		return
	}

	if len(uids) == 0 {
		fmt.Println("📭 No new unread emails.")
		return
	}

	// Limit to last 10 emails to avoid overwhelming the system
	const maxEmails = 10
	if len(uids) > maxEmails {
		fmt.Printf("⚠️ Too many emails (%d). Fetching last %d only.\n", len(uids), maxEmails)
		uids = uids[len(uids)-maxEmails:]
	}

	fmt.Printf("📧 Fetching %d new emails...\n", len(uids))

	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope, imap.FetchFlags, imap.FetchUid}

	messages := make(chan *imap.Message)
	done := make(chan error, 1)

	go func() {
		done <- c.imapClient.Fetch(seqset, items, messages)
	}()

	for msg := range messages {
		c.processMessage(msg, section)

		// Mark as seen
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		flags := []interface{}{imap.SeenFlag}
		seq := new(imap.SeqSet)
		seq.AddNum(msg.Uid)
		if err := c.imapClient.Store(seq, item, flags, nil); err != nil {
			logger.ErrorCF("email", "Failed to mark email as seen", map[string]interface{}{"uid": msg.Uid})
		}
	}

	if err := <-done; err != nil {
		logger.ErrorCF("email", "Fetch failed", map[string]interface{}{"error": err.Error()})
	}
}

func (c *EmailChannel) connectIMAP() error {
	addr := fmt.Sprintf("%s:%d", c.config.IMAPServer, c.config.IMAPPort)
	logger.DebugCF("email", "Connecting to IMAP", map[string]interface{}{"addr": addr})

	var err error
	if c.config.IMAPPort == 993 {
		c.imapClient, err = client.DialTLS(addr, nil)
	} else {
		c.imapClient, err = client.Dial(addr)
	}
	if err != nil {
		return err
	}

	if err := c.imapClient.Login(c.config.IMAPUser, c.config.IMAPPassword); err != nil {
		return err
	}

	return nil
}

func (c *EmailChannel) processMessage(msg *imap.Message, section *imap.BodySectionName) {
	if msg == nil {
		return
	}

	sender := "unknown"
	if len(msg.Envelope.From) > 0 {
		sender = fmt.Sprintf("%s@%s", msg.Envelope.From[0].MailboxName, msg.Envelope.From[0].HostName)
	}

	subject := msg.Envelope.Subject

	// Check allowlist
	if !c.IsAllowed(sender) {
		logger.DebugCF("email", "Message rejected by allowlist", map[string]interface{}{"sender": sender})
		return
	}

	r := msg.GetBody(section)
	if r == nil {
		logger.WarnC("email", "Message has no body")
		return
	}

	// Create a new mail reader
	mr, err := mail.CreateReader(r)
	if err != nil {
		logger.ErrorCF("email", "Failed to create mail reader", map[string]interface{}{"error": err.Error()})
		return
	}

	body := ""

	// Read each part
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			logger.ErrorCF("email", "Failed to read email part", map[string]interface{}{"error": err.Error()})
			break
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			// checks explicitly for text/plain
			contentType, _, _ := h.ContentType()
			if contentType == "text/plain" {
				b, _ := io.ReadAll(p.Body)
				body += string(b)
			}
		case *mail.AttachmentHeader:
			// Handle attachments if needed
		}
	}

	if body == "" {
		// Try to read simple body if multipart failed or wasn't multipart
		// Reset reader if possible or handle non-multipart - simplified for this implementation
		// For the MVP, we assume most emails have a text/plain part.
		body = "[Content could not be parsed or was empty]"
	}

	content := fmt.Sprintf("Subject: %s\n\n%s", subject, body)

	// Sender is the "User ID", ChatID is also the sender email for direct replies
	chatID := sender // In email, the chat ID is effectively the sender's address

	c.HandleMessage(sender, chatID, content, nil, map[string]string{
		"subject": subject,
		"email":   sender,
	})
	fmt.Printf("✅ Processed email from %s: %s\n", sender, subject)
}

// HandleCustomCommand handles internal commands
// HandleCustomCommand handles internal commands
func (c *EmailChannel) CheckNow() {
	select {
	case c.manualCheck <- true:
		logger.InfoC("email", "Manual check triggered")
	default:
		logger.DebugC("email", "Manual check already queued, skipping")
	}
}

func (c *EmailChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	logger.DebugCF("email", "Send received", map[string]interface{}{"content": msg.Content, "chat_id": msg.ChatID})

	// Check for command (handle potential whitespace)
	if strings.TrimSpace(msg.Content) == "CMD:CHECK" {
		c.CheckNow()
		return nil
	}

	addr := fmt.Sprintf("%s:%d", c.config.SMTPServer, c.config.SMTPPort)
	auth := smtp.PlainAuth("", c.config.SMTPUser, c.config.SMTPPassword, c.config.SMTPServer)

	to := []string{msg.ChatID}

	// Simple email construction
	subject := "Re: PicoClaw Response"
	// If we preserved the original subject in session metadata, we could use "Re: " + original_subject

	body := fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", msg.ChatID, subject, msg.Content)

	// Handle TLS for port 465 (SMTPS) vs 587 (STARTTLS)
	if c.config.SMTPPort == 465 {
		// Direct TLS
		tlsConfig := &tls.Config{
			ServerName: c.config.SMTPServer,
		}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return err
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, c.config.SMTPServer)
		if err != nil {
			return err
		}
		defer client.Quit()

		if err = client.Auth(auth); err != nil {
			return err
		}
		if err = client.Mail(c.config.SMTPUser); err != nil {
			return err
		}
		if err = client.Rcpt(to[0]); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(body))
		if err != nil {
			return err
		}
		return w.Close()
	} else {
		// STARTTLS (Standard for 587)
		return smtp.SendMail(addr, auth, c.config.SMTPUser, to, []byte(body))
	}
}
