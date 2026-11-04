package passwords

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Vaultwarden reads a Vaultwarden or Bitwarden server through its HTTP API, so
// it needs no external CLI and works in the distroless image (ADR-0014).
//
// The server stores only ciphertext: the master password is what decrypts it,
// and a Keycloak login cannot replace it. Lumen derives the keys in memory,
// sends the server only the password hash, and decrypts one field at a time.
type Vaultwarden struct {
	resolve SecretResolver
	now     func() time.Time
}

func (*Vaultwarden) Kind() string { return "vaultwarden" }

func (*Vaultwarden) Title() string { return "Vaultwarden / Bitwarden (API)" }

func (*Vaultwarden) ConfigSchema() []Field {
	return []Field{
		{Key: "server", Label: "Адрес сервера", Type: FieldURL, Required: true,
			Placeholder: "https://vaultwarden.example.com"},
		{Key: "email", Label: "Почта учётной записи", Type: FieldText, Required: true,
			Placeholder: "you@example.com",
			Help:        "Участвует в получении ключа из мастер-пароля"},
		{Key: "client_id", Label: "API-ключ: client_id", Type: FieldText,
			Placeholder: "user.xxxxxxxx-xxxx-…",
			Help:        "Настройки аккаунта → Безопасность → Ключи → API-ключ. Нужен, если включена двухфакторная защита"},
		{Key: "client_secret", Label: "API-ключ: client_secret", Type: FieldSecret,
			Placeholder: "",
			Help:        "Вставьте значение: оно сохранится зашифрованным. Можно и ссылкой: env:ИМЯ"},
		{Key: "allow_http", Label: "Разрешить HTTP без шифрования", Type: FieldBool,
			Help: "Только для внутренней сети: по соединению идут хеш пароля и токены"},
	}
}

func (*Vaultwarden) Validate(c Config) error {
	if _, err := vwBase(c); err != nil {
		return err
	}
	if !strings.Contains(c.Value("email", ""), "@") {
		return errors.New("укажите почту учётной записи")
	}
	if (c.Value("client_id", "") == "") != (c.Value("client_secret", "") == "") {
		return errors.New("для API-ключа нужны и client_id, и client_secret")
	}
	return nil
}

func (*Vaultwarden) NeedsPassphrase(Config) bool { return true }

// vwBase checks the address: a master password hash must not cross the network
// in the clear by accident.
func vwBase(c Config) (string, error) {
	raw := strings.TrimRight(strings.TrimSpace(c.Value("server", "")), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("адрес сервера указывается как https://vaultwarden.example.com")
	}
	if u.Scheme == "http" && !c.Bool("allow_http") {
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", errors.New("адрес без HTTPS: включите «Разрешить HTTP», только если это внутренняя сеть")
		}
	}
	return raw, nil
}

// vwSession is what an unlocked store keeps in memory: the keys and the
// still encrypted items. Every read decrypts only what it needs.
type vwSession struct {
	mu       sync.Mutex
	cfg      Config
	base     string
	client   *http.Client
	email    string
	token    string
	refresh  string
	expires  time.Time
	userKey  symKey
	orgKeys  map[string]symKey
	folders  map[string]string
	ciphers  []vwCipher
	syncedAt time.Time
}

// JSON field names match case-insensitively, which covers both the PascalCase
// of older servers and the camelCase of current ones.
type vwSync struct {
	Profile struct {
		Key           string `json:"key"`
		PrivateKey    string `json:"privateKey"`
		Organizations []struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"organizations"`
	} `json:"profile"`
	Folders []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"folders"`
	Ciphers []vwCipher `json:"ciphers"`
}

type vwField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  int    `json:"type"`
}

type vwCipher struct {
	ID             string    `json:"id"`
	Type           int       `json:"type"`
	FolderID       string    `json:"folderId"`
	OrganizationID string    `json:"organizationId"`
	DeletedDate    string    `json:"deletedDate"`
	Key            string    `json:"key"`
	Name           string    `json:"name"`
	Notes          string    `json:"notes"`
	Fields         []vwField `json:"fields"`
	Login          *struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
		URIs     []struct {
			URI string `json:"uri"`
		} `json:"uris"`
	} `json:"login"`
	Card *struct {
		CardholderName string `json:"cardholderName"`
		Number         string `json:"number"`
		ExpMonth       string `json:"expMonth"`
		ExpYear        string `json:"expYear"`
		Code           string `json:"code"`
	} `json:"card"`
	Identity map[string]any `json:"identity"`
	SSHKey   *struct {
		PrivateKey string `json:"privateKey"`
		PublicKey  string `json:"publicKey"`
	} `json:"sshKey"`
}

var vwTypeNames = map[int]string{1: "login", 2: "note", 3: "card", 4: "identity", 5: "ssh"}

// vwRequest does one call. It never follows redirects: a redirected request
// would carry the password hash or the token to wherever the answer points.
func (s *vwSession) do(ctx context.Context, method, path string, body io.Reader, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		// The URL error holds only the address; the form body is not part of it.
		return nil, 0, fmt.Errorf("сервер %s недоступен: %w", s.base, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	return data, resp.StatusCode, err
}

func (s *vwSession) preLogin(ctx context.Context) (vwKDF, error) {
	var kdf vwKDF
	payload, _ := json.Marshal(map[string]string{"email": s.email})
	headers := map[string]string{"Content-Type": "application/json"}
	data, status, err := s.do(ctx, http.MethodPost, "/identity/accounts/prelogin", strings.NewReader(string(payload)), headers)
	if err == nil && status == http.StatusNotFound {
		data, status, err = s.do(ctx, http.MethodPost, "/api/accounts/prelogin", strings.NewReader(string(payload)), headers)
	}
	if err != nil {
		return kdf, err
	}
	if status != http.StatusOK {
		return kdf, fmt.Errorf("сервер не вернул параметры ключа (HTTP %d) — это точно Vaultwarden?", status)
	}
	if err := json.Unmarshal(data, &kdf); err != nil {
		return kdf, fmt.Errorf("не удалось разобрать параметры ключа: %w", err)
	}
	return kdf, nil
}

// deviceID stays the same for a store, so the server sees one device and does
// not send a "new device" letter on every unlock.
func deviceID(storeID string) string {
	sum := sha256.Sum256([]byte("lumen:" + storeID))
	h := fmt.Sprintf("%x", sum[:16])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// tokenRequest asks for an access token with one of the three grants.
func (s *vwSession) tokenRequest(ctx context.Context, form url.Values) error {
	form.Set("deviceType", "25") // Linux CLI: the closest type for a server process
	form.Set("deviceIdentifier", deviceID(s.cfg.ID))
	form.Set("deviceName", "Lumen")
	headers := map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
		"Auth-Email":   base64.RawURLEncoding.EncodeToString([]byte(s.email)),
	}
	data, status, err := s.do(ctx, http.MethodPost, "/identity/connect/token", strings.NewReader(form.Encode()), headers)
	if err != nil {
		return err
	}
	var answer struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TwoFactor    []any  `json:"TwoFactorProviders"`
		Description  string `json:"error_description"`
	}
	_ = json.Unmarshal(data, &answer)
	if status != http.StatusOK || answer.AccessToken == "" {
		if len(answer.TwoFactor) > 0 {
			return errors.New("у учётной записи включена двухфакторная защита: укажите API-ключ в настройках хранилища")
		}
		if form.Get("grant_type") == "client_credentials" {
			return fmt.Errorf("сервер отклонил API-ключ (HTTP %d)", status)
		}
		return fmt.Errorf("сервер отклонил вход (HTTP %d): проверьте почту и мастер-пароль", status)
	}
	s.token = answer.AccessToken
	if answer.RefreshToken != "" {
		s.refresh = answer.RefreshToken
	}
	s.expires = time.Now().Add(time.Duration(answer.ExpiresIn) * time.Second)
	return nil
}

func (v *Vaultwarden) apiKeyForm(ctx context.Context, cfg Config) (url.Values, error) {
	if v.resolve == nil {
		return nil, errors.New("ссылки на секреты недоступны")
	}
	secret, err := v.resolve(ctx, cfg.Value("client_secret", ""))
	if err != nil {
		return nil, fmt.Errorf("client_secret: %w", err)
	}
	return url.Values{
		"grant_type":    {"client_credentials"},
		"scope":         {"api"},
		"client_id":     {cfg.Value("client_id", "")},
		"client_secret": {strings.TrimSpace(string(secret))},
	}, nil
}

// Unlock signs in and turns the master password into the account keys. The
// password is not kept; a wrong one fails on the key's authentication tag.
func (v *Vaultwarden) Unlock(ctx context.Context, cfg Config, passphrase []byte, _ Runner) (Session, error) {
	if len(passphrase) == 0 {
		return Session{}, errors.New("нужен мастер-пароль")
	}
	base, err := vwBase(cfg)
	if err != nil {
		return Session{}, err
	}
	s := &vwSession{
		cfg: cfg, base: base, email: strings.ToLower(strings.TrimSpace(cfg.Value("email", ""))),
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	kdf, err := s.preLogin(ctx)
	if err != nil {
		return Session{}, err
	}
	master, err := deriveMasterKey(passphrase, s.email, kdf)
	if err != nil {
		return Session{}, err
	}
	var form url.Values
	if cfg.Value("client_id", "") != "" {
		if form, err = v.apiKeyForm(ctx, cfg); err != nil {
			return Session{}, err
		}
	} else {
		form = url.Values{
			"grant_type": {"password"},
			"scope":      {"api offline_access"},
			"client_id":  {"cli"},
			"username":   {s.email},
			"password":   {masterPasswordHash(master, passphrase)},
		}
	}
	if err := s.tokenRequest(ctx, form); err != nil {
		return Session{}, err
	}

	data, err := s.fetchSync(ctx)
	if err != nil {
		return Session{}, err
	}
	stretched, err := stretchMasterKey(master)
	if err != nil {
		return Session{}, err
	}
	raw, err := decryptString(data.Profile.Key, stretched)
	if err != nil {
		return Session{}, errors.New("неверный мастер-пароль")
	}
	if s.userKey, err = newSymKey(raw); err != nil {
		return Session{}, err
	}
	s.applySync(data)
	return Session{Data: s}, nil
}

func (s *vwSession) fetchSync(ctx context.Context) (*vwSync, error) {
	data, status, err := s.do(ctx, http.MethodGet, "/api/sync?excludeDomains=true", nil,
		map[string]string{"Authorization": "Bearer " + s.token})
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		return nil, errVWExpired
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("сервер не отдал записи (HTTP %d)", status)
	}
	var out vwSync
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("не удалось разобрать записи: %w", err)
	}
	return &out, nil
}

var errVWExpired = errors.New("вход на сервере истёк: откройте хранилище заново")

// applySync replaces the snapshot. An organization whose key fails stays out
// of orgKeys; its items then show as undecryptable rather than breaking the rest.
func (s *vwSession) applySync(data *vwSync) {
	s.orgKeys = map[string]symKey{}
	if data.Profile.PrivateKey != "" {
		if der, err := decryptString(data.Profile.PrivateKey, s.userKey); err == nil {
			for _, org := range data.Profile.Organizations {
				if raw, err := decryptOrgKey(org.Key, der); err == nil {
					if key, err := newSymKey(raw); err == nil {
						s.orgKeys[org.ID] = key
					}
				}
			}
		}
	}
	s.folders = map[string]string{}
	for _, f := range data.Folders {
		if name, err := decryptString(f.Name, s.userKey); err == nil {
			s.folders[f.ID] = string(name)
		}
	}
	s.ciphers = s.ciphers[:0]
	for _, c := range data.Ciphers {
		if c.DeletedDate == "" {
			s.ciphers = append(s.ciphers, c)
		}
	}
	s.syncedAt = time.Now()
}

// freshen re-reads the items when the snapshot is old, renewing the token first
// if it has expired. A store that stays open sees changes made elsewhere.
func (v *Vaultwarden) freshen(ctx context.Context, s *vwSession) error {
	if time.Since(s.syncedAt) < 30*time.Second {
		return nil
	}
	if time.Now().After(s.expires.Add(-time.Minute)) {
		if err := v.renew(ctx, s); err != nil {
			return err
		}
	}
	data, err := s.fetchSync(ctx)
	if errors.Is(err, errVWExpired) {
		if err = v.renew(ctx, s); err == nil {
			data, err = s.fetchSync(ctx)
		}
	}
	if err != nil {
		return err
	}
	s.applySync(data)
	return nil
}

func (v *Vaultwarden) renew(ctx context.Context, s *vwSession) error {
	if s.cfg.Value("client_id", "") != "" {
		form, err := v.apiKeyForm(ctx, s.cfg)
		if err != nil {
			return err
		}
		return s.tokenRequest(ctx, form)
	}
	if s.refresh == "" {
		return errVWExpired
	}
	if err := s.tokenRequest(ctx, url.Values{
		"grant_type": {"refresh_token"}, "client_id": {"cli"}, "refresh_token": {s.refresh},
	}); err != nil {
		return errVWExpired
	}
	return nil
}

// itemKey finds the key of one item: its own if it has one, wrapped by the key
// of its organization or of the user.
func (s *vwSession) itemKey(c *vwCipher) (symKey, error) {
	parent := s.userKey
	if c.OrganizationID != "" {
		k, ok := s.orgKeys[c.OrganizationID]
		if !ok {
			return symKey{}, errors.New("нет ключа организации")
		}
		parent = k
	}
	if c.Key == "" {
		return parent, nil
	}
	raw, err := decryptString(c.Key, parent)
	if err != nil {
		return symKey{}, err
	}
	return newSymKey(raw)
}

func text(enc string, key symKey) string {
	if enc == "" {
		return ""
	}
	out, err := decryptString(enc, key)
	if err != nil {
		return ""
	}
	return string(out)
}

func (v *Vaultwarden) List(ctx context.Context, _ Config, sess Session, query string, _ Runner) ([]Item, error) {
	s, ok := sess.Data.(*vwSession)
	if !ok {
		return nil, ErrLocked
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := v.freshen(ctx, s); err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	items := make([]Item, 0, len(s.ciphers))
	for i := range s.ciphers {
		item := s.describe(&s.ciphers[i])
		if needle == "" || strings.Contains(strings.ToLower(item.Name+"\x00"+item.Username+"\x00"+item.URI+"\x00"+item.Folder), needle) {
			items = append(items, item)
		}
	}
	return items, nil
}

// describe builds the list entry: names and addresses, and which secret fields
// exist, without decrypting their values.
func (s *vwSession) describe(c *vwCipher) Item {
	item := Item{ID: c.ID, Type: vwTypeNames[c.Type], Folder: s.folders[c.FolderID], Fields: []string{}}
	key, err := s.itemKey(c)
	if err != nil {
		item.Name = "(не удалось расшифровать)"
		return item
	}
	if item.Name = text(c.Name, key); item.Name == "" {
		item.Name = "(не удалось расшифровать)"
		return item
	}
	add := func(enc, field string) {
		if enc != "" {
			item.Fields = append(item.Fields, field)
		}
	}
	if c.Login != nil {
		item.Username = text(c.Login.Username, key)
		add(c.Login.Username, "username")
		add(c.Login.Password, "password")
		add(c.Login.TOTP, "totp")
		if len(c.Login.URIs) > 0 {
			item.URI = text(c.Login.URIs[0].URI, key)
			add(c.Login.URIs[0].URI, "uri")
		}
	}
	if c.Card != nil {
		add(c.Card.CardholderName, "cardholder")
		add(c.Card.Number, "number")
		add(c.Card.Code, "code")
		add(c.Card.ExpMonth, "exp_month")
		add(c.Card.ExpYear, "exp_year")
	}
	identity := make([]string, 0, len(c.Identity))
	for k, val := range c.Identity {
		if enc, ok := val.(string); ok && isEncString(enc) {
			identity = append(identity, "identity:"+k)
		}
	}
	sort.Strings(identity)
	item.Fields = append(item.Fields, identity...)
	if c.SSHKey != nil {
		add(c.SSHKey.PublicKey, "public_key")
		add(c.SSHKey.PrivateKey, "private_key")
	}
	add(c.Notes, "notes")
	for _, f := range c.Fields {
		if name := text(f.Name, key); name != "" && f.Value != "" {
			item.Fields = append(item.Fields, "field:"+name)
		}
	}
	return item
}

func isEncString(s string) bool {
	return len(s) > 2 && s[1] == '.' && s[0] >= '0' && s[0] <= '9'
}

func (v *Vaultwarden) Reveal(_ context.Context, _ Config, sess Session, itemID, field string, _ Runner) (string, error) {
	s, ok := sess.Data.(*vwSession)
	if !ok {
		return "", ErrLocked
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.ciphers {
		if s.ciphers[i].ID == itemID {
			return v.reveal(s, &s.ciphers[i], field)
		}
	}
	return "", fmt.Errorf("%w: %s", ErrNoField, field)
}

func (v *Vaultwarden) reveal(s *vwSession, c *vwCipher, field string) (string, error) {
	key, err := s.itemKey(c)
	if err != nil {
		return "", err
	}
	enc := ""
	switch {
	case field == "notes":
		enc = c.Notes
	case strings.HasPrefix(field, "field:"):
		for _, f := range c.Fields {
			if text(f.Name, key) == strings.TrimPrefix(field, "field:") {
				enc = f.Value
				break
			}
		}
	case strings.HasPrefix(field, "identity:"):
		enc, _ = c.Identity[strings.TrimPrefix(field, "identity:")].(string)
	case c.Login != nil && field == "username":
		enc = c.Login.Username
	case c.Login != nil && field == "password":
		enc = c.Login.Password
	case c.Login != nil && field == "totp":
		enc = c.Login.TOTP
	case c.Login != nil && field == "uri" && len(c.Login.URIs) > 0:
		enc = c.Login.URIs[0].URI
	case c.Card != nil && field == "cardholder":
		enc = c.Card.CardholderName
	case c.Card != nil && field == "number":
		enc = c.Card.Number
	case c.Card != nil && field == "code":
		enc = c.Card.Code
	case c.Card != nil && field == "exp_month":
		enc = c.Card.ExpMonth
	case c.Card != nil && field == "exp_year":
		enc = c.Card.ExpYear
	case c.SSHKey != nil && field == "public_key":
		enc = c.SSHKey.PublicKey
	case c.SSHKey != nil && field == "private_key":
		enc = c.SSHKey.PrivateKey
	}
	if enc == "" {
		return "", fmt.Errorf("%w: %s", ErrNoField, field)
	}
	raw, err := decryptString(enc, key)
	if err != nil {
		return "", fmt.Errorf("не удалось расшифровать поле %s", field)
	}
	if field == "totp" {
		now := time.Now
		if v.now != nil {
			now = v.now
		}
		return totpCode(string(raw), now())
	}
	return string(raw), nil
}
