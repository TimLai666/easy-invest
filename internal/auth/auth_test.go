package auth

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 密碼雜湊
// ---------------------------------------------------------------------------

func TestHashAndVerifyPassword(t *testing.T) {
	tests := []struct {
		name    string
		plain   string
		verify  string
		wantOK  bool
	}{
		{
			name:   "正確密碼應驗證通過",
			plain:  "my-secure-pass-123",
			verify: "my-secure-pass-123",
			wantOK: true,
		},
		{
			name:   "錯誤密碼應回傳 false",
			plain:  "my-secure-pass-123",
			verify: "wrong-password-xxx",
			wantOK: false,
		},
		{
			name:   "空字串密碼驗證錯誤密碼應回傳 false",
			plain:  "",
			verify: "anything",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashPassword(tt.plain)
			if err != nil {
				t.Fatalf("HashPassword 失敗: %v", err)
			}
			// 雜湊值不應等於明文
			if hash == tt.plain {
				t.Fatal("hash 不應等於明文密碼")
			}
			// 雜湊應以 $argon2id$ 開頭
			if !strings.HasPrefix(hash, "$argon2id$") {
				t.Fatalf("hash 格式不對: %s", hash)
			}
			got := VerifyPassword(tt.verify, hash)
			if got != tt.wantOK {
				t.Fatalf("VerifyPassword(%q) = %v, want %v", tt.verify, got, tt.wantOK)
			}
		})
	}
}

// 同一明文每次產生的 hash 應不同（因 salt 隨機）。
func TestHashPasswordUsesRandomSalt(t *testing.T) {
	h1, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatal("兩次雜湊結果不應相同（salt 隨機）")
	}
}

// ---------------------------------------------------------------------------
// API Key 產生與前綴
// ---------------------------------------------------------------------------

func TestNewAPIKeyToken(t *testing.T) {
	tests := []struct {
		name string
		check func(t *testing.T, key string)
	}{
		{
			name: "前綴應為 ei_",
			check: func(t *testing.T, key string) {
				if !strings.HasPrefix(key, "ei_") {
					t.Fatalf("key 前綴應為 ei_，got %q", key[:6])
				}
			},
		},
		{
			name: "長度足夠（base64(32 bytes) + 3 字元前綴）",
			check: func(t *testing.T, key string) {
				// 32 bytes → base64 RawURL = 43 chars + "ei_" = 46
				if len(key) < 40 {
					t.Fatalf("key 長度太短: %d", len(key))
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := NewAPIKeyToken()
			if err != nil {
				t.Fatalf("NewAPIKeyToken 失敗: %v", err)
			}
			tt.check(t, key)
		})
	}
}

func TestHashAPIKeyDiffersFromPlaintext(t *testing.T) {
	key, err := NewAPIKeyToken()
	if err != nil {
		t.Fatal(err)
	}
	hash := HashAPIKey(key)
	if hash == key {
		t.Fatal("HashAPIKey 不應等於明文")
	}
	// SHA256 hex = 64 字元
	if len(hash) != 64 {
		t.Fatalf("hash 長度 = %d, want 64", len(hash))
	}
}

func TestPrefixAPIKey(t *testing.T) {
	key, err := NewAPIKeyToken()
	if err != nil {
		t.Fatal(err)
	}
	prefix := PrefixAPIKey(key)
	if len(prefix) != 11 {
		t.Fatalf("prefix 長度 = %d, want 11", len(prefix))
	}
	if prefix != key[:11] {
		t.Fatalf("prefix = %q, want %q", prefix, key[:11])
	}
}

// 短字串不截斷。
func TestPrefixAPIKeyShortString(t *testing.T) {
	short := "abc"
	prefix := PrefixAPIKey(short)
	if prefix != short {
		t.Fatalf("prefix = %q, want %q", prefix, short)
	}
}

// ---------------------------------------------------------------------------
// Session token 簽發與驗證
// ---------------------------------------------------------------------------

func newTestService() *Service {
	secret := []byte("test-secret-key-0123456789abcdef") // 32 bytes
	return NewService(nil, secret, true)
}

func TestSignAndVerifySession(t *testing.T) {
	svc := newTestService()
	tests := []struct {
		name      string
		userID    string
		expiresAt time.Time
		wantOK    bool
		wantID    string
	}{
		{
			name:      "有效 token 回傳正確 userID",
			userID:    "user-abc-123",
			expiresAt: time.Now().Add(1 * time.Hour),
			wantOK:    true,
			wantID:    "user-abc-123",
		},
		{
			name:      "過期 token 驗證失敗",
			userID:    "user-expired",
			expiresAt: time.Now().Add(-1 * time.Hour),
			wantOK:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := svc.signSession(tt.userID, tt.expiresAt)
			gotID, gotOK := svc.verifySession(token)
			if gotOK != tt.wantOK {
				t.Fatalf("verifySession ok = %v, want %v", gotOK, tt.wantOK)
			}
			if tt.wantOK && gotID != tt.wantID {
				t.Fatalf("verifySession userID = %q, want %q", gotID, tt.wantID)
			}
		})
	}
}

// 竄改 token 應驗證失敗。
func TestVerifySessionTamperedToken(t *testing.T) {
	svc := newTestService()
	token := svc.signSession("user-123", time.Now().Add(1*time.Hour))

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "竄改 payload 部分",
			token: "dGFtcGVyZWQ." + strings.Split(token, ".")[1],
		},
		{
			name:  "竄改 signature 部分",
			token: strings.Split(token, ".")[0] + ".dGFtcGVyZWQ",
		},
		{
			name:  "完全亂碼",
			token: "not-a-valid-token",
		},
		{
			name:  "空字串",
			token: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := svc.verifySession(tt.token)
			if ok {
				t.Fatal("竄改或無效的 token 應驗證失敗")
			}
		})
	}
}

// 不同 secret 簽出的 token 應互相驗不過。
func TestVerifySessionWrongSecret(t *testing.T) {
	svc1 := NewService(nil, []byte("secret-AAAAAAAAAAAAAAAAAAAAAAA"), true)
	svc2 := NewService(nil, []byte("secret-BBBBBBBBBBBBBBBBBBBBBBB"), true)

	token := svc1.signSession("user-123", time.Now().Add(1*time.Hour))
	_, ok := svc2.verifySession(token)
	if ok {
		t.Fatal("不同 secret 簽出的 token 不應驗證通過")
	}
}

// ---------------------------------------------------------------------------
// Principal.HasScope
// ---------------------------------------------------------------------------

func TestPrincipalHasScope(t *testing.T) {
	tests := []struct {
		name     string
		principal Principal
		scope    string
		want     bool
	}{
		{
			name:      "session 模式一律回 true",
			principal: Principal{Via: "session"},
			scope:     "anything",
			want:      true,
		},
		{
			name:      "api_key 模式擁有的 scope 回 true",
			principal: Principal{Via: "api_key", Scopes: []string{"read:portfolio", "write:trade"}},
			scope:     "read:portfolio",
			want:      true,
		},
		{
			name:      "api_key 模式沒有的 scope 回 false",
			principal: Principal{Via: "api_key", Scopes: []string{"read:portfolio"}},
			scope:     "write:trade",
			want:      false,
		},
		{
			name:      "api_key 模式空 scopes 回 false",
			principal: Principal{Via: "api_key", Scopes: nil},
			scope:     "read:portfolio",
			want:      false,
		},
		{
			name:      "session 模式不管 scope 清單",
			principal: Principal{Via: "session", Scopes: []string{}},
			scope:     "admin:nuclear",
			want:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.principal.HasScope(tt.scope)
			if got != tt.want {
				t.Fatalf("HasScope(%q) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// context 存取
// ---------------------------------------------------------------------------

func TestPrincipalContextRoundTrip(t *testing.T) {
	p := Principal{UserID: "u1", Email: "a@b.c", Via: "session"}
	ctx := WithPrincipal(t.Context(), p)
	got, ok := PrincipalFromContext(ctx)
	if !ok {
		t.Fatal("PrincipalFromContext 應回 true")
	}
	if got.UserID != p.UserID || got.Email != p.Email || got.Via != p.Via {
		t.Fatalf("got %+v, want %+v", got, p)
	}
}

func TestPrincipalFromContextMissing(t *testing.T) {
	_, ok := PrincipalFromContext(t.Context())
	if ok {
		t.Fatal("空 context 應回 false")
	}
}
