package mailer

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// Message is an email's subject and its two bodies
type Message struct {
	Subject string
	Text    string
	HTML    string
}

// Lifetimes the messages state. They match the tokens' (auth.ResetTokenLifetime and the like),
// which the callers pass in, so the text can't say one thing while the token does another.

// ResetPassword is the email with a password reset link. Account emails are in English: the
// server does not know the account's language.
func ResetPassword(link string, lifetime time.Duration) Message {
	return message("Reset your Peekaping password",
		"Someone asked to reset the password of your Peekaping account.",
		"Choose a new password",
		link,
		fmt.Sprintf("The link works once and expires in %s. If you didn't ask for it, ignore this email: your password stays as it is.", duration(lifetime)))
}

// VerifyEmail is the email with an address verification link
func VerifyEmail(link string, lifetime time.Duration) Message {
	return message("Verify your email address for Peekaping",
		"Confirm that this is your email address for Peekaping.",
		"Verify your email address",
		link,
		fmt.Sprintf("The link works once and expires in %s. If you didn't create a Peekaping account, ignore this email.", duration(lifetime)))
}

// Invite is the email with the link of a workspace invite. inviter may be empty, for an invite
// made with an API key.
func Invite(workspaceName, inviter, role, link string, lifetime time.Duration) Message {
	who := "You were invited"
	if inviter != "" {
		who = inviter + " invited you"
	}
	return message(fmt.Sprintf("You're invited to %s on Peekaping", workspaceName),
		fmt.Sprintf("%s to join the workspace %q on Peekaping as %s.", who, workspaceName, article(role)),
		"Accept the invite",
		link,
		fmt.Sprintf("The link works once and expires in %s. If you weren't expecting it, ignore this email.", duration(lifetime)))
}

func message(subject, intro, action, link, footer string) Message {
	text := intro + "\n\n" + action + ":\n" + link + "\n\n" + footer + "\n"
	page := "<p>" + html.EscapeString(intro) + "</p>\n" +
		"<p><a href=\"" + html.EscapeString(link) + "\">" + html.EscapeString(action) + "</a></p>\n" +
		"<p>If the button doesn't work, copy this address into your browser:<br>" + html.EscapeString(link) + "</p>\n" +
		"<p>" + html.EscapeString(footer) + "</p>\n"
	return Message{Subject: subject, Text: text, HTML: page}
}

// duration says a lifetime the way the messages do: "1 hour", "3 days"
func duration(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return plural(int(d/(24*time.Hour)), "day")
	case d%time.Hour == 0:
		return plural(int(d/time.Hour), "hour")
	}
	return plural(int(d/time.Minute), "minute")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func article(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return "a member"
	}
	if strings.ContainsAny(role[:1], "aeiou") {
		return "an " + role
	}
	return "a " + role
}
