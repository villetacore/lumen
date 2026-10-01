package service

import (
	"fmt"
	"log/slog"

	"github.com/alexpyslar/lumen/server/internal/crypt"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

// MigrationResult reports what a vault-wide conversion did.
type MigrationResult struct {
	Converted int
	Skipped   int
}

// EncryptVault rewrites every plaintext note as an encrypted container. It is
// safe to re-run: already encrypted files are skipped, so an interrupted
// migration can simply be repeated.
func EncryptVault(v *vault.Vault, codec vault.Codec, log *slog.Logger) (MigrationResult, error) {
	return migrate(v, log, func(rel string, raw []byte) ([]byte, bool, error) {
		if crypt.IsEncrypted(raw) {
			return nil, false, nil
		}
		out, err := codec.Encode(rel, raw)
		return out, true, err
	})
}

// DecryptVault turns an encrypted vault back into plain markdown, so the user
// can always walk away with ordinary files (ADR-0001).
func DecryptVault(v *vault.Vault, codec vault.Codec, log *slog.Logger) (MigrationResult, error) {
	return migrate(v, log, func(rel string, raw []byte) ([]byte, bool, error) {
		if !crypt.IsEncrypted(raw) {
			return nil, false, nil
		}
		out, err := codec.Decode(rel, raw)
		return out, true, err
	})
}

func migrate(
	v *vault.Vault,
	log *slog.Logger,
	convert func(rel string, raw []byte) ([]byte, bool, error),
) (MigrationResult, error) {
	var res MigrationResult
	var failed error

	err := v.Walk(func(fi vault.FileInfo) error {
		raw, err := v.ReadRaw(fi.Path)
		if err != nil {
			failed = fmt.Errorf("read %s: %w", fi.Path, err)
			return failed
		}
		out, changed, err := convert(fi.Path, raw)
		if err != nil {
			failed = fmt.Errorf("convert %s: %w", fi.Path, err)
			return failed
		}
		if !changed {
			res.Skipped++
			return nil
		}
		if err := v.WriteRaw(fi.Path, out); err != nil {
			failed = fmt.Errorf("write %s: %w", fi.Path, err)
			return failed
		}
		res.Converted++
		log.Debug("converted", "path", fi.Path)
		return nil
	})
	if failed != nil {
		return res, failed
	}
	return res, err
}
