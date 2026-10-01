package passwords

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRunner answers from a table and records what was asked, so tests never
// touch a real password manager.
type fakeRunner struct {
	replies map[string]string
	fail    map[string]string
	calls   []Input
}

func (f *fakeRunner) Run(_ context.Context, in Input) ([]byte, error) {
	f.calls = append(f.calls, in)
	key := in.Name + " " + strings.Join(in.Args, " ")
	if msg, ok := f.fail[key]; ok {
		return nil, errors.New(msg)
	}
	if out, ok := f.replies[key]; ok {
		return []byte(out), nil
	}
	return nil, errors.New("неожиданная команда: " + key)
}

func newManager(t *testing.T, r *fakeRunner) *Manager {
	t.Helper()
	m := NewManager(t.TempDir(), r, nil)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m
}

const bwItemsJSON = `[
  {"id":"i1","name":"GitHub","notes":"","login":{"username":"alex","password":"s3cret","totp":"otpauth://x","uris":[{"uri":"https://github.com"}]}},
  {"id":"i2","name":"Роутер","notes":"адрес 192.168.1.1","login":{"username":"admin","password":"pw","uris":[]}}
]`

func TestBitwardenUnlockAndList(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"bw unlock --raw --passwordenv LUMEN_BW_PASSWORD --nointeraction": "session-token-1\n",
		"bw status --session session-token-1 --nointeraction":             `{"status":"unlocked"}`,
		"bw list items --session session-token-1 --nointeraction":         bwItemsJSON,
	}}
	m := newManager(t, r)
	ctx := context.Background()

	cfg, err := m.Add(Config{Kind: "bitwarden", Name: "Мой Vaultwarden"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ID != "moy-vaultwarden" {
		t.Errorf("id = %q", cfg.ID)
	}

	// Пока не разблокировано — список недоступен.
	if _, err := m.Items(ctx, cfg.ID, ""); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}

	if err := m.Unlock(ctx, cfg.ID, []byte("мастер-пароль")); err != nil {
		t.Fatal(err)
	}

	// Мастер-пароль обязан идти через окружение, а не через аргументы.
	unlockCall := r.calls[0]
	if unlockCall.Env["LUMEN_BW_PASSWORD"] != "мастер-пароль" {
		t.Errorf("пароль не передан через окружение: %+v", unlockCall.Env)
	}
	if strings.Contains(strings.Join(unlockCall.Args, " "), "мастер-пароль") {
		t.Error("мастер-пароль попал в аргументы процесса — их видит вся система")
	}

	items, err := m.Items(ctx, cfg.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	if items[0].Name != "GitHub" || items[0].Username != "alex" {
		t.Errorf("item = %+v", items[0])
	}
	if items[0].URI != "https://github.com" {
		t.Errorf("uri = %q", items[0].URI)
	}
}

// Пароли не должны попадать в список — только признак того, что поле есть.
func TestListNeverCarriesSecretValues(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"bw status --session ready --nointeraction":     `{"status":"unlocked"}`,
		"bw list items --session ready --nointeraction": bwItemsJSON,
	}}
	m := newManager(t, r)
	ctx := context.Background()

	cfg, _ := m.Add(Config{Kind: "bitwarden", Name: "bw", Values: map[string]string{"session": "literal:ready"}})
	if err := m.Unlock(ctx, cfg.ID, []byte("ready")); err != nil {
		t.Fatal(err)
	}
	items, err := m.Items(ctx, cfg.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		for _, field := range []string{it.Name, it.Username, it.URI, it.Folder} {
			if strings.Contains(field, "s3cret") || strings.Contains(field, "otpauth") {
				t.Fatalf("значение секрета утекло в список: %+v", it)
			}
		}
	}
	if !contains(items[0].Fields, "password") || !contains(items[0].Fields, "totp") {
		t.Errorf("не отмечены доступные поля: %v", items[0].Fields)
	}
}

func TestBitwardenReveal(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"bw status --session ready --nointeraction":                `{"status":"unlocked"}`,
		"bw get password i1 --session ready --nointeraction --raw": "s3cret\n",
		"bw get totp i1 --session ready --nointeraction --raw":     "123456\n",
	}}
	m := newManager(t, r)
	ctx := context.Background()

	cfg, _ := m.Add(Config{Kind: "bitwarden", Name: "bw", Values: map[string]string{"session": "literal:ready"}})
	if err := m.Unlock(ctx, cfg.ID, []byte("ready")); err != nil {
		t.Fatal(err)
	}

	pw, err := m.Reveal(ctx, cfg.ID, "i1", "password")
	if err != nil || pw != "s3cret" {
		t.Fatalf("password = %q, err = %v", pw, err)
	}
	totp, err := m.Reveal(ctx, cfg.ID, "i1", "totp")
	if err != nil || totp != "123456" {
		t.Fatalf("totp = %q, err = %v", totp, err)
	}
	if _, err := m.Reveal(ctx, cfg.ID, "i1", "private_key"); !errors.Is(err, ErrNoField) {
		t.Errorf("произвольное поле не должно приниматься: %v", err)
	}
}

func TestBitwardenRefusesLockedVault(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"bw status --session ready --nointeraction": `{"status":"locked"}`,
	}}
	m := newManager(t, r)
	cfg, _ := m.Add(Config{Kind: "bitwarden", Name: "bw", Values: map[string]string{"session": "literal:ready"}})

	err := m.Unlock(context.Background(), cfg.ID, []byte("ready"))
	if err == nil || !strings.Contains(err.Error(), "bw login") {
		t.Fatalf("ошибка должна подсказывать про bw login: %v", err)
	}
}

func TestKeePassUnlockListReveal(t *testing.T) {
	db := `D:\keys\personal.kdbx`
	r := &fakeRunner{replies: map[string]string{
		"keepassxc-cli ls --quiet " + db:                                                             "Работа/\n",
		"keepassxc-cli ls --quiet --recursive --flatten " + db:                                       "Работа/\nРабота/GitHub\nДом/Роутер\n",
		"keepassxc-cli show --quiet --show-protected --attributes Password " + db + " Работа/GitHub": "s3cret\n",
		"keepassxc-cli search --quiet " + db + " git":                                                "Работа/GitHub\n",
	}}
	m := newManager(t, r)
	ctx := context.Background()

	cfg, err := m.Add(Config{Kind: "keepass", Name: "KeePass", Values: map[string]string{"db": db}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Unlock(ctx, cfg.ID, []byte("пароль базы")); err != nil {
		t.Fatal(err)
	}
	// Пароль базы уходит в stdin, а не в аргументы.
	if string(r.calls[0].Stdin) != "пароль базы\n" {
		t.Errorf("stdin = %q", r.calls[0].Stdin)
	}

	items, err := m.Items(ctx, cfg.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("группы не должны попадать в список записей: %+v", items)
	}
	if items[0].Name != "GitHub" || items[0].Folder != "Работа" {
		t.Errorf("item = %+v", items[0])
	}

	pw, err := m.Reveal(ctx, cfg.ID, "Работа/GitHub", "password")
	if err != nil || pw != "s3cret" {
		t.Fatalf("password = %q, err = %v", pw, err)
	}

	found, err := m.Items(ctx, cfg.ID, "git")
	if err != nil || len(found) != 1 {
		t.Fatalf("поиск = %+v, err = %v", found, err)
	}
}

func TestKeePassValidation(t *testing.T) {
	m := newManager(t, &fakeRunner{})
	if _, err := m.Add(Config{Kind: "keepass", Name: "нет базы"}); err == nil {
		t.Error("путь к базе обязателен")
	}
	if _, err := m.Add(Config{Kind: "keepass", Name: "ключ", Values: map[string]string{
		"db": "a.kdbx", "no_password": "true",
	}}); err == nil {
		t.Error("без пароля нужен файл-ключ")
	}
}

func TestAutoLock(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"bw status --session ready --nointeraction": `{"status":"unlocked"}`,
	}}
	m := newManager(t, r)
	m.SetAutoLock(30 * time.Millisecond)
	ctx := context.Background()

	cfg, _ := m.Add(Config{Kind: "bitwarden", Name: "bw", Values: map[string]string{"session": "literal:ready"}})
	if err := m.Unlock(ctx, cfg.ID, []byte("ready")); err != nil {
		t.Fatal(err)
	}
	if !m.List()[0].Unlocked {
		t.Fatal("после разблокировки хранилище должно быть открыто")
	}

	time.Sleep(60 * time.Millisecond)
	if m.List()[0].Unlocked {
		t.Error("хранилище должно было закрыться само")
	}
	if _, err := m.Items(ctx, cfg.ID, ""); !errors.Is(err, ErrLocked) {
		t.Errorf("want ErrLocked, got %v", err)
	}
}

func TestLockAndLockAll(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"bw status --session ready --nointeraction": `{"status":"unlocked"}`,
	}}
	m := newManager(t, r)
	ctx := context.Background()
	cfg, _ := m.Add(Config{Kind: "bitwarden", Name: "bw", Values: map[string]string{"session": "literal:ready"}})

	_ = m.Unlock(ctx, cfg.ID, []byte("ready"))
	m.Lock(cfg.ID)
	if m.List()[0].Unlocked {
		t.Error("Lock не сработал")
	}

	_ = m.Unlock(ctx, cfg.ID, []byte("ready"))
	m.LockAll()
	if m.List()[0].Unlocked {
		t.Error("LockAll не сработал")
	}
}

// Файл конфигурации не должен содержать ни паролей, ни сессий.
func TestConfigFileHoldsNoSecrets(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"bw unlock --raw --passwordenv LUMEN_BW_PASSWORD --nointeraction": "session-token-1\n",
		"bw status --session session-token-1 --nointeraction":             `{"status":"unlocked"}`,
	}}
	root := t.TempDir()
	m := NewManager(root, r, nil)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}

	cfg, _ := m.Add(Config{Kind: "bitwarden", Name: "Vaultwarden",
		Values: map[string]string{"server": "https://vault.example.com"}})
	if err := m.Unlock(context.Background(), cfg.ID, []byte("очень-секретный-пароль")); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ConfigFile)))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"очень-секретный-пароль", "session-token-1"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("секрет попал в %s:\n%s", ConfigFile, raw)
		}
	}
	if !strings.Contains(string(raw), "vault.example.com") {
		t.Errorf("настройки не сохранились:\n%s", raw)
	}
}

func TestStorePersistsAcrossRestart(t *testing.T) {
	root := t.TempDir()
	first := NewManager(root, &fakeRunner{}, nil)
	if err := first.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Add(Config{Kind: "keepass", Name: "Личное",
		Values: map[string]string{"db": "a.kdbx"}}); err != nil {
		t.Fatal(err)
	}

	second := NewManager(root, &fakeRunner{}, nil)
	if err := second.Load(); err != nil {
		t.Fatal(err)
	}
	list := second.List()
	if len(list) != 1 || list[0].Values["db"] != "a.kdbx" {
		t.Fatalf("list = %+v", list)
	}
	if list[0].Unlocked {
		t.Error("после перезапуска хранилище обязано быть закрыто")
	}
}

func TestDeleteUnknown(t *testing.T) {
	m := newManager(t, &fakeRunner{})
	if err := m.Delete("нет-такого"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
