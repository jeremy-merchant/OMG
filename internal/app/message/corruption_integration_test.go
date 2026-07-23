package message

import (
	"context"
	"example.invalid/coordledger/internal/app/testsupport"
	coord "example.invalid/coordledger/internal/domain/coordination"
	"testing"
	"time"
)

func TestDeliveryLifecycleFailsClosedForCorruptTimestamp(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s, db := testsupport.Store(t, now)
	testsupport.Seed(t, s, now)
	svc := New(s, func() time.Time { return now })
	to := coord.RecipientTarget{SessionID: "target"}
	m := testsupport.Message("m", coord.MessageNotice, to, "body")
	if _, err := svc.Send(context.Background(), "send", testsupport.Project, m); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Deliver(context.Background(), "deliver", "m", to); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE message_recipients SET delivered_at='not-a-time' WHERE message_id='m'"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Read(context.Background(), "read", "m", to)
	if err == nil || got.MessageID != "" {
		t.Fatalf("partial corrupt delivery=%+v err=%v", got, err)
	}
}
