package config

import (
	"reflect"
	"testing"
)

func TestContactEmailIsFixedForUserFacingMail(t *testing.T) {
	t.Setenv("MAIL_FROM", "Other <support@example.test>")
	t.Setenv("MAIL_REPLY_TO", "support@example.test")

	cfg := Load()
	if cfg.MailFrom != "BotTrade <jyron@mail.bot-trade.org>" {
		t.Fatalf("MailFrom = %q", cfg.MailFrom)
	}
	if cfg.MailReplyTo != "jyron@bot-trade.org" {
		t.Fatalf("MailReplyTo = %q", cfg.MailReplyTo)
	}
}

func TestSplitCSV(t *testing.T) {
	want := []string{"price_old_one", "price_old_two"}
	if got := splitCSV(" price_old_one, price_old_two ,, "); !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCSV = %#v, want %#v", got, want)
	}
}
