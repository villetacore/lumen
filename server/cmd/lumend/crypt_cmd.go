package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/alexpyslar/lumen/server/internal/crypt"
	"github.com/alexpyslar/lumen/server/internal/service"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

const cryptUsage = `lumend crypt <команда> [флаги]

  status    показать, зашифрован ли vault
  init      создать ключ и зашифровать все существующие заметки
  encrypt   дошифровать заметки, оставшиеся в открытом виде
  decrypt   расшифровать vault обратно в обычный markdown
  passwd    сменить парольную фразу (файлы не переписываются)

Флаги:
  --vault <путь>                папка vault (или LUMEN_VAULT)
  --passphrase-from <ссылка>    откуда взять фразу; см. ниже
  --passphrase-file <файл>      то же, но только файл
  --new-passphrase-from <ссылка>  новая фраза для passwd
  --new-passphrase-file <файл>    то же, но только файл

Ссылки на секреты:
  env:LUMEN_PASSPHRASE
  file:/run/secrets/lumen
  command:pass show lumen/vault
  bitwarden:lumen-vault                 (работает и с Vaultwarden через bw CLI)
  keepass:D:/db.kdbx#Lumen/vault?password=env:KDBX_PASSWORD
`

type cryptFlags struct {
	vault      string
	passRef    string
	passFile   string
	newPassRef string
	newPassFil string
}

func (f *cryptFlags) source() passphraseSource {
	return passphraseSource{Ref: f.passRef, File: f.passFile, EnvKey: "LUMEN_PASSPHRASE"}
}

func (f *cryptFlags) newSource() passphraseSource {
	return passphraseSource{Ref: f.newPassRef, File: f.newPassFil, EnvKey: "LUMEN_NEW_PASSPHRASE"}
}

func parseCryptFlags(cmd string, args []string) (*cryptFlags, error) {
	fs := flag.NewFlagSet("crypt "+cmd, flag.ContinueOnError)
	c := &cryptFlags{}
	fs.StringVar(&c.vault, "vault", envOr("LUMEN_VAULT", "./vault"), "path to the vault directory")
	fs.StringVar(&c.passRef, "passphrase-from", os.Getenv("LUMEN_PASSPHRASE_FROM"),
		"where to take the passphrase: env:VAR, file:PATH, command:..., bitwarden:ITEM, keepass:DB#ENTRY")
	fs.StringVar(&c.passFile, "passphrase-file", os.Getenv("LUMEN_PASSPHRASE_FILE"), "file with the passphrase")
	fs.StringVar(&c.newPassRef, "new-passphrase-from", os.Getenv("LUMEN_NEW_PASSPHRASE_FROM"), "where to take the new passphrase")
	fs.StringVar(&c.newPassFil, "new-passphrase-file", os.Getenv("LUMEN_NEW_PASSPHRASE_FILE"), "file with the new passphrase")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(c.vault)
	if err != nil {
		return nil, err
	}
	c.vault = abs
	return c, nil
}

func keyPath(vaultRoot string) string {
	return filepath.Join(vaultRoot, filepath.FromSlash(crypt.KeyFileName))
}

func runCrypt(args []string, log *slog.Logger) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, cryptUsage)
		return errors.New("crypt: не указана команда")
	}
	cmd, rest := args[0], args[1:]
	flags, err := parseCryptFlags(cmd, rest)
	if err != nil {
		return err
	}

	switch cmd {
	case "status":
		return cryptStatus(flags)
	case "init":
		return cryptInit(flags, log)
	case "encrypt":
		return cryptConvert(flags, log, true)
	case "decrypt":
		return cryptConvert(flags, log, false)
	case "passwd":
		return cryptPasswd(flags)
	case "help", "-h", "--help":
		fmt.Fprint(os.Stdout, cryptUsage)
		return nil
	default:
		fmt.Fprint(os.Stderr, cryptUsage)
		return fmt.Errorf("crypt: неизвестная команда %q", cmd)
	}
}

func cryptStatus(f *cryptFlags) error {
	v, err := vault.Open(f.vault)
	if err != nil {
		return err
	}
	kp := keyPath(v.Root)
	if !crypt.Exists(kp) {
		fmt.Printf("vault:      %s\nшифрование: выключено\n", v.Root)
		return nil
	}

	// Считаем, сколько файлов уже в контейнерах — так видно незавершённую миграцию.
	var total, encrypted int
	_ = v.Walk(func(fi vault.FileInfo) error {
		raw, err := v.ReadRaw(fi.Path)
		if err != nil {
			return nil
		}
		total++
		if crypt.IsEncrypted(raw) {
			encrypted++
		}
		return nil
	})
	fmt.Printf("vault:      %s\nшифрование: включено (%s)\nзаметок:    %d, зашифровано %d\n",
		v.Root, crypt.KeyFileName, total, encrypted)
	if encrypted < total {
		fmt.Println("подсказка:  часть файлов открыта, выполните `lumend crypt encrypt`")
	}
	return nil
}

func cryptInit(f *cryptFlags, log *slog.Logger) error {
	v, err := vault.Open(f.vault)
	if err != nil {
		return err
	}
	kp := keyPath(v.Root)
	if crypt.Exists(kp) {
		return fmt.Errorf("%w: %s", crypt.ErrAlreadyInit, kp)
	}

	pass, err := readPassphrase(f.source(), "Новая парольная фраза для vault: ", true)
	if err != nil {
		return err
	}
	keyring, err := crypt.Init(kp, pass)
	if err != nil {
		return err
	}
	fmt.Printf("Ключ создан: %s\n", kp)

	res, err := service.EncryptVault(v, keyring, log)
	if err != nil {
		return fmt.Errorf("шифрование vault прервано (%d файлов уже обработано): %w", res.Converted, err)
	}
	fmt.Printf("Зашифровано файлов: %d\n", res.Converted)
	fmt.Println("Потеря парольной фразы означает потерю заметок — сохраните её и резервную копию ключа.")
	return nil
}

func cryptConvert(f *cryptFlags, log *slog.Logger, encrypt bool) error {
	v, err := vault.Open(f.vault)
	if err != nil {
		return err
	}
	kp := keyPath(v.Root)
	if !crypt.Exists(kp) {
		return fmt.Errorf("%w: %s", crypt.ErrNotEncrypted, kp)
	}
	pass, err := readPassphrase(f.source(), "Парольная фраза: ", false)
	if err != nil {
		return err
	}
	keyring, err := crypt.Load(kp, pass)
	if err != nil {
		return err
	}

	if encrypt {
		res, err := service.EncryptVault(v, keyring, log)
		if err != nil {
			return err
		}
		fmt.Printf("Зашифровано: %d, уже было: %d\n", res.Converted, res.Skipped)
		return nil
	}

	res, err := service.DecryptVault(v, keyring, log)
	if err != nil {
		return err
	}
	fmt.Printf("Расшифровано: %d, уже было открыто: %d\n", res.Converted, res.Skipped)
	fmt.Printf("Ключ оставлен в %s — удалите его, если шифрование больше не нужно.\n", crypt.KeyFileName)
	return nil
}

func cryptPasswd(f *cryptFlags) error {
	v, err := vault.Open(f.vault)
	if err != nil {
		return err
	}
	kp := keyPath(v.Root)
	if !crypt.Exists(kp) {
		return fmt.Errorf("%w: %s", crypt.ErrNotEncrypted, kp)
	}
	old, err := readPassphrase(f.source(), "Текущая парольная фраза: ", false)
	if err != nil {
		return err
	}
	next, err := readPassphrase(f.newSource(), "Новая парольная фраза: ", true)
	if err != nil {
		return err
	}
	if err := crypt.ChangePassphrase(kp, old, next); err != nil {
		return err
	}
	fmt.Println("Парольная фраза изменена; файлы vault не переписывались.")
	return nil
}

// unlockVault installs the encryption codec when the vault has a key file.
func unlockVault(v *vault.Vault, src passphraseSource, log *slog.Logger) error {
	kp := keyPath(v.Root)
	if !crypt.Exists(kp) {
		return nil
	}
	pass, err := readPassphrase(src, "Парольная фраза vault: ", false)
	if err != nil {
		return err
	}
	keyring, err := crypt.Load(kp, pass)
	if err != nil {
		return err
	}
	v.SetCodec(keyring)
	log.Info("vault unlocked", "key", crypt.KeyFileName)
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
