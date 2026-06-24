package provider

import (
	"context"
	"errors"
	"testing"

	"cloud.google.com/go/iam"
	iampb "cloud.google.com/go/iam/apiv1/iampb"
)

func memberOfRole(policy *iam.Policy3, role string) []string {
	for _, b := range policy.Bindings {
		if b.Role == role {
			return b.Members
		}
	}
	return nil
}

func hasMember(members []string, want string) bool {
	for _, m := range members {
		if m == want {
			return true
		}
	}
	return false
}

func TestAddPublicReadBinding(t *testing.T) {
	t.Run("empty policy adds binding", func(t *testing.T) {
		policy := &iam.Policy3{}
		if !addPublicReadBinding(policy) {
			t.Fatal("expected policy to be modified")
		}
		if !hasMember(memberOfRole(policy, publicReadRole), allUsers) {
			t.Errorf("allUsers not granted %s: %+v", publicReadRole, policy.Bindings)
		}
	})

	t.Run("existing binding is a no-op", func(t *testing.T) {
		policy := &iam.Policy3{
			Bindings: []*iampb.Binding{
				{Role: publicReadRole, Members: []string{allUsers}},
			},
		}
		if addPublicReadBinding(policy) {
			t.Error("expected no modification when binding already exists")
		}
		members := memberOfRole(policy, publicReadRole)
		if got := len(members); got != 1 {
			t.Errorf("members = %v, want exactly [allUsers]", members)
		}
	})

	t.Run("appends allUsers to existing role binding", func(t *testing.T) {
		policy := &iam.Policy3{
			Bindings: []*iampb.Binding{
				{Role: publicReadRole, Members: []string{"user:alice@example.com"}},
			},
		}
		if !addPublicReadBinding(policy) {
			t.Fatal("expected policy to be modified")
		}
		members := memberOfRole(policy, publicReadRole)
		if !hasMember(members, allUsers) || !hasMember(members, "user:alice@example.com") {
			t.Errorf("members = %v, want both alice and allUsers", members)
		}
	})

	t.Run("preserves other role bindings", func(t *testing.T) {
		policy := &iam.Policy3{
			Bindings: []*iampb.Binding{
				{Role: "roles/storage.admin", Members: []string{"user:bob@example.com"}},
			},
		}
		if !addPublicReadBinding(policy) {
			t.Fatal("expected policy to be modified")
		}
		if !hasMember(memberOfRole(policy, "roles/storage.admin"), "user:bob@example.com") {
			t.Error("admin binding was dropped")
		}
		if !hasMember(memberOfRole(policy, publicReadRole), allUsers) {
			t.Error("public-read binding not added")
		}
	})
}

// fakeIAMHandle is a test double for iamV3Handle.
type fakeIAMHandle struct {
	policy    *iam.Policy3
	policyErr error
	setErr    error
	setCalls  int
}

func (f *fakeIAMHandle) Policy(context.Context) (*iam.Policy3, error) {
	if f.policyErr != nil {
		return nil, f.policyErr
	}
	return f.policy, nil
}

func (f *fakeIAMHandle) SetPolicy(_ context.Context, p *iam.Policy3) error {
	f.setCalls++
	if f.setErr != nil {
		return f.setErr
	}
	f.policy = p
	return nil
}

func TestEnsurePublicRead(t *testing.T) {
	t.Run("grants when not public", func(t *testing.T) {
		h := &fakeIAMHandle{policy: &iam.Policy3{}}
		if err := ensurePublicRead(context.Background(), h); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h.setCalls != 1 {
			t.Errorf("SetPolicy calls = %d, want 1", h.setCalls)
		}
		if !hasMember(memberOfRole(h.policy, publicReadRole), allUsers) {
			t.Error("allUsers not granted after ensurePublicRead")
		}
	})

	t.Run("no SetPolicy when already public", func(t *testing.T) {
		h := &fakeIAMHandle{policy: &iam.Policy3{
			Bindings: []*iampb.Binding{
				{Role: publicReadRole, Members: []string{allUsers}},
			},
		}}
		if err := ensurePublicRead(context.Background(), h); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h.setCalls != 0 {
			t.Errorf("SetPolicy calls = %d, want 0", h.setCalls)
		}
	})

	t.Run("propagates Policy error", func(t *testing.T) {
		want := errors.New("boom")
		h := &fakeIAMHandle{policyErr: want}
		if err := ensurePublicRead(context.Background(), h); !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
	})

	t.Run("propagates SetPolicy error", func(t *testing.T) {
		want := errors.New("denied")
		h := &fakeIAMHandle{policy: &iam.Policy3{}, setErr: want}
		if err := ensurePublicRead(context.Background(), h); !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
	})
}
