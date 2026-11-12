package auth_test

import (
	"context"
	"errors"
	"testing"

	"go.icco.me/glanced-api/internal/auth"
	"go.icco.me/glanced-api/internal/db"
	"go.icco.me/glanced-api/internal/dbtest"
)

func TestCreateUserDuplicateEmail(t *testing.T) {
	g := dbtest.New(t)
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, g, "ada@example.com", pw); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateUser(ctx, g, "ADA@example.com", pw); !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("err = %v", err)
	}
}

func TestListAndRevokeSessions(t *testing.T) {
	g := dbtest.New(t)
	ctx := context.Background()
	u, err := auth.CreateUser(ctx, g, "ada@example.com", pw)
	if err != nil {
		t.Fatal(err)
	}
	phone, err := auth.LoginAgent(ctx, g, "ada@example.com", pw, "Glanced Android")
	if err != nil {
		t.Fatal(err)
	}
	web, err := auth.LoginAgent(ctx, g, "ada@example.com", pw, "Firefox")
	if err != nil {
		t.Fatal(err)
	}
	list, err := auth.ListSessions(ctx, g, u.ID, web)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("sessions = %+v", list)
	}
	var phoneID string
	for _, s := range list {
		if s.UserAgent == "Glanced Android" {
			phoneID = s.ID
			if s.Current {
				t.Error("phone marked current")
			}
		} else if !s.Current {
			t.Error("web session not marked current")
		}
	}
	if err := auth.RevokeSession(ctx, g, "0199a000-0000-7000-8000-00000000000b", phoneID); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.UserForToken(ctx, g, phone); err != nil {
		t.Errorf("another user's revoke removed the session: %v", err)
	}
	if err := auth.RevokeSession(ctx, g, u.ID, phoneID); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.UserForToken(ctx, g, phone); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("revoked token still valid: %v", err)
	}
	if got, err := auth.UserForToken(ctx, g, web); err != nil || got.ID != u.ID {
		t.Errorf("web token: %v %v", got, err)
	}
}

func TestChangePassword(t *testing.T) {
	g := dbtest.New(t)
	ctx := context.Background()
	u, err := auth.CreateUser(ctx, g, "ada@example.com", pw)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.ChangePassword(ctx, g, u.ID, "wrong password!", "a brand new passphrase"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("wrong old: %v", err)
	}
	if err := auth.ChangePassword(ctx, g, u.ID, pw, "short"); !errors.Is(err, auth.ErrWeakPassword) {
		t.Errorf("weak: %v", err)
	}
	if err := auth.ChangePassword(ctx, g, u.ID, pw, "a brand new passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Login(ctx, g, "ada@example.com", "a brand new passphrase"); err != nil {
		t.Errorf("login with new password: %v", err)
	}
	if _, err := auth.Login(ctx, g, "ada@example.com", pw); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("old password still works: %v", err)
	}
}

func TestDeleteUserRemovesEverything(t *testing.T) {
	g := dbtest.New(t)
	ctx := context.Background()
	u, err := auth.CreateUser(ctx, g, "ada@example.com", pw)
	if err != nil {
		t.Fatal(err)
	}
	other, err := auth.CreateUser(ctx, g, "bob@example.com", pw)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Login(ctx, g, "ada@example.com", pw)
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{u.ID, other.ID} {
		g.Create(&db.Card{UserID: uid, ID: "next", Kind: "next", DeviceID: "d"})
		g.Create(&db.Line{UserID: uid, ID: "l1", CardID: "next", Signal: "empty", DeviceID: "d"})
	}
	if err := auth.DeleteUser(ctx, g, u.ID, "wrong password!"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("wrong password: %v", err)
	}
	if err := auth.DeleteUser(ctx, g, u.ID, pw); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.UserForToken(ctx, g, tok); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("session survived: %v", err)
	}
	var cards, lines, users int64
	g.Model(&db.Card{}).Count(&cards)
	g.Model(&db.Line{}).Count(&lines)
	g.Model(&db.User{}).Count(&users)
	if cards != 1 || lines != 1 || users != 1 {
		t.Errorf("left cards=%d lines=%d users=%d, want only bob's", cards, lines, users)
	}
}
