package provider

import (
	"testing"

	"cloud.google.com/go/iam"
	iampb "cloud.google.com/go/iam/apiv1/iampb"
	expr "google.golang.org/genproto/googleapis/type/expr"
)

func memberOfRole(policy *iam.Policy3, role string) []string {
	for _, b := range policy.Bindings {
		if b.Role == role && b.Condition == nil {
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

func TestHasPublicReadBinding(t *testing.T) {
	tests := []struct {
		name   string
		policy *iam.Policy3
		want   bool
	}{
		{
			name:   "empty",
			policy: &iam.Policy3{},
			want:   false,
		},
		{
			name: "unconditional allUsers",
			policy: &iam.Policy3{Bindings: []*iampb.Binding{
				{Role: publicReadRole, Members: []string{allUsers}},
			}},
			want: true,
		},
		{
			name: "conditional allUsers is not public",
			policy: &iam.Policy3{Bindings: []*iampb.Binding{
				{
					Role:      publicReadRole,
					Members:   []string{allUsers},
					Condition: &expr.Expr{Expression: "true"},
				},
			}},
			want: false,
		},
		{
			name: "other member only",
			policy: &iam.Policy3{Bindings: []*iampb.Binding{
				{Role: publicReadRole, Members: []string{"user:alice@example.com"}},
			}},
			want: false,
		},
		{
			name: "other role",
			policy: &iam.Policy3{Bindings: []*iampb.Binding{
				{Role: "roles/storage.admin", Members: []string{allUsers}},
			}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPublicReadBinding(tt.policy); got != tt.want {
				t.Errorf("hasPublicReadBinding = %v, want %v", got, tt.want)
			}
		})
	}
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

	t.Run("existing unconditional binding is a no-op", func(t *testing.T) {
		policy := &iam.Policy3{
			Bindings: []*iampb.Binding{
				{Role: publicReadRole, Members: []string{allUsers}},
			},
		}
		if addPublicReadBinding(policy) {
			t.Error("expected no modification when binding already exists")
		}
		if got := len(memberOfRole(policy, publicReadRole)); got != 1 {
			t.Errorf("members len = %d, want 1", got)
		}
	})

	t.Run("appends allUsers to existing unconditional role binding", func(t *testing.T) {
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

	t.Run("does not touch a conditional binding; adds a new unconditional one", func(t *testing.T) {
		cond := &expr.Expr{Expression: "resource.name.startsWith('x')"}
		policy := &iam.Policy3{
			Bindings: []*iampb.Binding{
				{Role: publicReadRole, Members: []string{"user:alice@example.com"}, Condition: cond},
			},
		}
		if !addPublicReadBinding(policy) {
			t.Fatal("expected policy to be modified")
		}
		// The conditional binding must be left untouched.
		var conditional, unconditional *iampb.Binding
		for _, b := range policy.Bindings {
			if b.Role != publicReadRole {
				continue
			}
			if b.Condition != nil {
				conditional = b
			} else {
				unconditional = b
			}
		}
		if conditional == nil {
			t.Fatal("conditional binding was dropped")
		}
		if hasMember(conditional.Members, allUsers) {
			t.Error("allUsers wrongly appended to conditional binding")
		}
		if unconditional == nil || !hasMember(unconditional.Members, allUsers) {
			t.Errorf("expected a new unconditional binding granting allUsers; got %+v", policy.Bindings)
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
