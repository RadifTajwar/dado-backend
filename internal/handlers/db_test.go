package handlers

/* Database tests. They need a throwaway MongoDB (never Atlas) and are skipped
   without one:

	docker run -d --rm -p 27018:27017 --name dado-test-mongo mongo:7
	DADO_TEST_MONGO_URI=mongodb://localhost:27018 go test ./internal/handlers -run DB
	docker stop dado-test-mongo

   Each test uses its own database and drops it afterwards. Email goes to a
   closed local port, so nothing is ever sent. */

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"dado/internal/auth"
	"dado/internal/config"
	"dado/internal/db"
	"dado/internal/mail"
	"dado/internal/models"
)

func testHandler(t *testing.T) *Handler {
	t.Helper()
	uri := os.Getenv("DADO_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set DADO_TEST_MONGO_URI to run database tests")
	}
	ctx := context.Background()
	d, err := db.Connect(ctx, uri, "dado_test_"+bson.NewObjectID().Hex())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = d.Clients.Database().Drop(ctx)
		_ = d.Close(ctx)
	})
	cfg := &config.Config{SiteURL: "http://localhost:3000", SMTPHost: "127.0.0.1", SMTPPort: "1", SMTPUser: "studio@example.com"}
	return New(d, cfg, mail.New(cfg))
}

// call runs one handler with an optional JSON body and {id} path value.
func call(h http.HandlerFunc, id, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestDBConvertTrialOnlyOnce(t *testing.T) {
	h := testHandler(t)
	ctx := context.Background()
	trial := models.Trial{ID: bson.NewObjectID(), Name: "Grace Mbeki", Email: "grace@example.com",
		Services: []string{"photo"}, Status: "flagged", FlagReason: "Checking", CreatedAt: models.Now()}
	if _, err := h.db.Trials.InsertOne(ctx, trial); err != nil {
		t.Fatal(err)
	}

	// eight admins click Convert at the same moment
	codes := make([]int, 8)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = call(h.ConvertTrial, trial.ID.Hex(), "").Code
		}()
	}
	wg.Wait()

	created := 0
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	n, _ := h.db.Clients.CountDocuments(ctx, bson.M{})
	if created != 1 || n != 1 {
		t.Fatalf("want exactly one conversion and one client, got %d and %d (%v)", created, n, codes)
	}

	var got models.Trial
	if err := h.db.Trials.FindOne(ctx, bson.M{"_id": trial.ID}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "converted" || got.FlagReason != "" || got.ClientID == nil {
		t.Errorf("trial after convert: %+v", got)
	}

	// deleting that client re-opens the trial with no leftover flag reason
	if _, err := h.db.Trials.UpdateOne(ctx, bson.M{"_id": trial.ID},
		bson.M{"$set": bson.M{"status": "flagged", "flagReason": "Spam?"}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req.SetPathValue("id", got.ClientID.Hex())
	rec := httptest.NewRecorder()
	h.DeleteClient(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete client: %d %s", rec.Code, rec.Body)
	}
	_ = h.db.Trials.FindOne(ctx, bson.M{"_id": trial.ID}).Decode(&got)
	if got.Status != "new" || got.FlagReason != "" || got.ClientID != nil {
		t.Errorf("trial after its client was deleted: %+v", got)
	}
}

func TestDBResetPassword(t *testing.T) {
	h := testHandler(t)
	ctx := context.Background()
	oldHash, _ := auth.HashPassword("old-password")
	expires := models.Now().Add(resetTTL)
	admin := models.Admin{ID: bson.NewObjectID(), Email: "admin@example.com", PasswordHash: oldHash,
		ResetTokenHash: auth.HashToken("the-token"), ResetExpires: &expires, CreatedAt: models.Now()}
	if _, err := h.db.Admins.InsertOne(ctx, admin); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		body string
		want int
	}{
		{`{"token":"wrong","password":"new-password"}`, http.StatusBadRequest},
		{`{"token":"","password":"new-password"}`, http.StatusBadRequest},
		{`{"token":"the-token","password":"short"}`, http.StatusBadRequest},
		{`{"token":"the-token","password":"new-password"}`, http.StatusOK},
		{`{"token":"the-token","password":"another-one"}`, http.StatusBadRequest}, // used up
	}
	for _, s := range steps {
		if rec := call(h.ResetPassword, "", s.body); rec.Code != s.want {
			t.Errorf("%s: got %d %s", s.body, rec.Code, rec.Body)
		}
	}

	var got models.Admin
	_ = h.db.Admins.FindOne(ctx, bson.M{"_id": admin.ID}).Decode(&got)
	if !auth.CheckPassword(got.PasswordHash, "new-password") || got.ResetTokenHash != "" || got.ResetExpires != nil {
		t.Errorf("after reset: password changed=%v token left=%q", auth.CheckPassword(got.PasswordHash, "new-password"), got.ResetTokenHash)
	}

	// an expired link is refused
	past := models.Now().Add(-time.Minute)
	_, _ = h.db.Admins.UpdateOne(ctx, bson.M{"_id": admin.ID}, bson.M{"$set": bson.M{
		"resetTokenHash": auth.HashToken("old-token"), "resetExpires": past}})
	if rec := call(h.ResetPassword, "", `{"token":"old-token","password":"new-password-2"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("expired token: got %d", rec.Code)
	}
}

func TestDBForgotAnswersBeforeWorking(t *testing.T) {
	h := testHandler(t)
	ctx := context.Background()
	admin := models.Admin{ID: bson.NewObjectID(), Email: "admin@example.com", PasswordHash: "x", CreatedAt: models.Now()}
	if _, err := h.db.Admins.InsertOne(ctx, admin); err != nil {
		t.Fatal(err)
	}

	for _, email := range []string{"nobody@example.com", "Admin@Example.com"} {
		if rec := call(h.ForgotPassword, "", `{"email":"`+email+`"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
			t.Errorf("%s: got %d %s", email, rec.Code, rec.Body)
		}
	}
	h.mail.Wait() // the background lookup (and the failed local send) is done

	var got models.Admin
	_ = h.db.Admins.FindOne(ctx, bson.M{"_id": admin.ID}).Decode(&got)
	if got.ResetTokenHash == "" || got.ResetExpires == nil {
		t.Error("forgot did not store a reset token for the admin")
	}
}
