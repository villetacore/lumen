// Command lumend is the whole Lumen application: it indexes a vault, serves the
// HTTP API and — when the web client has been built into it — the UI as well.
//
//	lumend                      запустить сервер и веб-интерфейс
//	lumend --vault ./notes --open
//	lumend crypt init           включить шифрование vault
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/alexpyslar/lumen/server/internal/auth"
	"github.com/alexpyslar/lumen/server/internal/config"
	"github.com/alexpyslar/lumen/server/internal/httpapi"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/passwords"
	"github.com/alexpyslar/lumen/server/internal/secrets"
	"github.com/alexpyslar/lumen/server/internal/service"
	"github.com/alexpyslar/lumen/server/internal/vault"
	"github.com/alexpyslar/lumen/server/internal/webui"
)

// version is overridden at build time: -ldflags "-X main.version=..."
var version = "0.1.0-dev"

const usage = `lumend — сервер и приложение Lumen

Использование:
  lumend [serve] [флаги]     запустить (команда по умолчанию)
  lumend crypt <команда>     шифрование vault, см. lumend crypt help
  lumend version

Основные флаги:
  --vault <путь>        папка vault           (LUMEN_VAULT, по умолчанию ./vault)
  --addr <host:port>    адрес                 (LUMEN_ADDR, 127.0.0.1:8787)
  --token <строка>      требовать Bearer-токен для /api (LUMEN_TOKEN)
  --ui=false            не отдавать встроенный веб-клиент
  --open                открыть браузер после старта
  --watch=false         не следить за изменениями файлов
  --debounce 250ms      пауза перед переиндексацией изменённого файла
  --poll 2s             интервал опроса, если fsnotify недоступен
  --passphrase-from <ссылка>
                        откуда взять фразу шифрованного vault:
                        env:VAR · file:ПУТЬ · command:... · bitwarden:ЗАПИСЬ
                        keepass:БАЗА.kdbx#Запись?password=env:KDBX_PASSWORD
  -v                    подробный лог запросов

Single sign-on (Keycloak и любой OIDC):
  --public-url          внешний адрес, https://lumen.example.com
  --oidc-issuer         https://keycloak.example.com/realms/main
  --oidc-client-id      lumen
  --oidc-client-secret  ссылка на секрет (для конфиденциального клиента)
  --oidc-roles          через запятую: кого пускать (роли или группы)
  --session-ttl 12h     срок жизни сессии в браузере
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lumend:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	log := newLogger(hasFlag(args, "-v"))

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "serve":
			return serve(args[1:], log)
		case "crypt":
			return runCrypt(args[1:], log)
		case "version":
			fmt.Println("lumend", version)
			return nil
		case "help":
			fmt.Print(usage)
			return nil
		default:
			fmt.Fprint(os.Stderr, usage)
			return fmt.Errorf("неизвестная команда %q", args[0])
		}
	}
	if hasFlag(args, "-h") || hasFlag(args, "--help") {
		fmt.Print(usage)
		return nil
	}
	return serve(args, log)
}

func serve(args []string, log *slog.Logger) error {
	cfg, err := config.Load(args)
	if err != nil {
		return err
	}
	if cfg.Verbose {
		log = newLogger(true)
	}

	v, err := vault.Open(cfg.VaultPath)
	if errors.Is(err, fs.ErrNotExist) {
		// First run against a new folder: lay out the folders the views expect.
		if err = initVault(cfg.VaultPath); err == nil {
			log.Info("created new vault", "path", cfg.VaultPath)
			v, err = vault.Open(cfg.VaultPath)
		}
	}
	if err != nil {
		return fmt.Errorf("open vault: %w", err)
	}
	// An encrypted vault is unlocked once, here; everything above the vault
	// package works with plaintext and never learns about the key (ADR-0005).
	src := passphraseSource{
		Ref: cfg.PassphraseRef, File: cfg.PassphraseFile, EnvKey: "LUMEN_PASSPHRASE",
	}
	if err := unlockVault(v, src, log); err != nil {
		return fmt.Errorf("unlock vault: %w", err)
	}

	store := index.NewMemory(v.Root)
	bus := service.NewBus()
	ix := service.NewIndexer(v, store, bus, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if _, err := ix.ReindexAll(ctx); err != nil {
		return fmt.Errorf("initial index: %w", err)
	}
	if cfg.WatchEnabled {
		go ix.Watch(ctx, cfg.Debounce, cfg.PollInterval)
	}

	oidcProvider, err := setupOIDC(ctx, cfg, log)
	if err != nil {
		return fmt.Errorf("keycloak/oidc: %w", err)
	}

	tasks := service.NewTasks(v, store, ix, bus, log)
	notes := service.NewNotes(v, store, ix, bus, log)
	integrationsSvc := service.NewIntegrations(v, store, tasks, notes, ix, bus, secrets.New(nil), log)
	if err := integrationsSvc.Load(); err != nil {
		log.Warn("не удалось прочитать конфигурацию интеграций", "err", err)
	}
	go integrationsSvc.Run(ctx)

	// Хранилища паролей: конфигурация лежит в vault, сессии — только в памяти.
	pwManager := passwords.NewManager(v.Root, nil, secrets.New(nil).Resolve)
	if err := pwManager.Load(); err != nil {
		log.Warn("не удалось прочитать конфигурацию хранилищ паролей", "err", err)
	}
	defer pwManager.LockAll()

	api := httpapi.New(cfg, store, v, ix, bus, integrationsSvc, oidcProvider, pwManager, log)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.ReadTimeout,
		// SSE streams must not be cut off by the write timeout.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	banner(cfg, v, log)
	if cfg.OpenBrowser && cfg.ServeUI {
		openBrowser(cfg.BaseURL(), log)
	}

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}

// setupOIDC prepares single sign-on when an issuer is configured. Discovery
// needs the provider to be reachable, so a typo or a sleeping Keycloak is
// reported at startup rather than at the first login.
func setupOIDC(ctx context.Context, cfg *config.Config, log *slog.Logger) (*auth.Provider, error) {
	if !cfg.OIDCEnabled() {
		return nil, nil
	}
	clientSecret := ""
	if ref := cfg.OIDCClientSecret; ref != "" {
		value, err := secrets.New(nil).Resolve(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("client secret: %w", err)
		}
		clientSecret = string(value)
	}

	insecure := strings.HasPrefix(cfg.PublicURL, "http://")
	provider, err := auth.NewProvider(ctx, auth.Config{
		Issuer:         cfg.OIDCIssuer,
		ClientID:       cfg.OIDCClientID,
		ClientSecret:   clientSecret,
		RedirectURL:    cfg.OIDCRedirectURL,
		Scopes:         cfg.OIDCScopes,
		RequiredRoles:  cfg.OIDCRoles,
		SessionTTL:     cfg.SessionTTL,
		InsecureCookie: insecure,
	}, nil)
	if err != nil {
		return nil, err
	}
	log.Info("single sign-on включён",
		"issuer", cfg.OIDCIssuer, "client", cfg.OIDCClientID,
		"redirect", cfg.OIDCRedirectURL, "роли", cfg.OIDCRoles)
	if insecure && !cfg.LocalOnly() {
		log.Warn("public-url без https: cookie сессии пойдёт без флага Secure",
			"public_url", cfg.PublicURL)
	}
	return provider, nil
}

func banner(cfg *config.Config, v *vault.Vault, log *slog.Logger) {
	ui := "выключен"
	switch {
	case !cfg.ServeUI:
		ui = "отключён флагом --ui=false"
	case webui.Available():
		ui = cfg.BaseURL()
	default:
		ui = "не собран (make app), доступна только заглушка"
	}
	log.Info("lumen готов",
		"версия", version,
		"vault", v.Root,
		"шифрование", v.Encrypted(),
		"api", cfg.BaseURL()+"/api/v1",
		"интерфейс", ui,
		"авторизация", cfg.Token != "",
	)
}

// openBrowser is best effort: failing to launch a browser must not stop the server.
func openBrowser(url string, log *slog.Logger) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Warn("не удалось открыть браузер", "url", url, "err", err)
		return
	}
	go func() { _ = cmd.Wait() }()
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

// initVault creates an empty vault with the folders the built-in views use:
// the inbox view reads 00-inbox/, projects live in 20-projects/ and quick
// capture writes daily notes to 40-daily/.
func initVault(root string) error {
	for _, dir := range []string{"00-inbox", "10-notes", "20-projects", "40-daily"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return err
		}
	}
	return nil
}
