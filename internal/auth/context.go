package auth

import "context"

type Principal struct {
	UserID   string
	Email    string
	APIKeyID string
	Scopes   []string
	Via      string
}

type contextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(contextKey{}).(Principal)
	return principal, ok
}

func (p Principal) HasScope(scope string) bool {
	if p.Via == "session" {
		return true
	}
	for _, item := range p.Scopes {
		if item == scope {
			return true
		}
	}
	return false
}
