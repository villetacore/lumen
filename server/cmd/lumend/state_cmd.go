package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/villetacore/lumen/server/internal/config"
	"github.com/villetacore/lumen/server/internal/state"
)

const stateUsage = `lumend state — операционная БД (ADR-0011)

  lumend state status [--state ПУТЬ | --vault ПУТЬ]
        путь, версия схемы, идентификатор экземпляра, проверка целостности
  lumend state backup <файл> [--state ПУТЬ | --vault ПУТЬ]
        согласованная копия; можно при работающем сервере
  lumend state restore <файл> [--state ПУТЬ | --vault ПУТЬ]
        заменить БД копией; сервер должен быть остановлен,
        текущая БД сохраняется в backups/

С PostgreSQL (LUMEN_DATABASE_URL):
  lumend state status
        версия схемы и идентификатор экземпляра
  lumend state import <файл lumen.db>
        перенести данные из SQLite в чистую базу; сервер должен быть остановлен
  backup и restore в этом режиме не работают: копии делает pg_dump.

Каталог по умолчанию — lumen-state рядом с vault (LUMEN_STATE, --state).
Бэкап vault делается отдельно, см. docs/OPERATIONS.md.
`

func runState(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(stateUsage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	var file string
	if cmd == "backup" || cmd == "restore" || cmd == "import" {
		if len(rest) == 0 || rest[0] == "" || rest[0][0] == '-' {
			return fmt.Errorf("lumend state %s: укажите файл", cmd)
		}
		file, rest = rest[0], rest[1:]
	}
	cfg, err := config.Load(rest)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if cfg.DatabaseURL != "" {
		return runStatePostgres(ctx, cmd, file, cfg.DatabaseURL)
	}
	switch cmd {
	case "import":
		return fmt.Errorf("импорт выполняется в PostgreSQL: задайте LUMEN_DATABASE_URL")
	case "status":
		info, err := state.Inspect(ctx, state.DBPath(cfg.StateDir))
		if errors.Is(err, os.ErrNotExist) {
			fmt.Printf("каталог: %s\nБД ещё не создана: она появится при первом запуске lumend\n", cfg.StateDir)
			return nil
		}
		if err != nil {
			return err
		}
		printInfo(info)
		backups, _ := filepath.Glob(filepath.Join(cfg.StateDir, "backups", "*.db"))
		fmt.Printf("автокопий в backups/: %d\n", len(backups))
		return nil
	case "backup":
		if err := state.BackupDir(ctx, cfg.StateDir, file); err != nil {
			return err
		}
		info, err := state.Inspect(ctx, file)
		if err != nil {
			return fmt.Errorf("копия создана, но не прошла проверку: %w", err)
		}
		fmt.Println("копия готова и проверена")
		printInfo(info)
		return nil
	case "restore":
		info, err := state.Restore(ctx, cfg.StateDir, file)
		if errors.Is(err, state.ErrLocked) {
			return fmt.Errorf("%w — остановите lumend и повторите", err)
		}
		if err != nil {
			return err
		}
		fmt.Println("БД восстановлена")
		if info.Replaced != "" {
			fmt.Println("прежняя БД сохранена:", info.Replaced)
		}
		printInfo(info)
		return nil
	default:
		fmt.Fprint(os.Stderr, stateUsage)
		return fmt.Errorf("неизвестная команда state %q", cmd)
	}
}

func runStatePostgres(ctx context.Context, cmd, file, url string) error {
	switch cmd {
	case "status":
		info, err := state.InspectPostgres(ctx, url)
		if err != nil {
			return err
		}
		fmt.Printf("база: PostgreSQL\nсхема: %d (этот lumend знает %d)\nэкземпляр: %s\n", info.Version, info.Latest, info.Instance)
		return nil
	case "backup", "restore":
		return state.ErrBackupByServer
	case "import":
		st, err := state.OpenPostgres(ctx, url, nil)
		if errors.Is(err, state.ErrLocked) {
			return fmt.Errorf("%w — остановите lumend и повторите", err)
		}
		if err != nil {
			return err
		}
		defer st.Close()
		counts, err := st.ImportSQLite(ctx, file)
		if err != nil {
			return err
		}
		fmt.Println("данные перенесены:")
		for _, t := range []string{"project_members", "users", "user_groups", "notifications", "issue_snapshots", "issue_events", "proposals"} {
			fmt.Printf("  %s: %d\n", t, counts[t])
		}
		return nil
	default:
		fmt.Fprint(os.Stderr, stateUsage)
		return fmt.Errorf("неизвестная команда state %q", cmd)
	}
}

func printInfo(info state.Info) {
	fmt.Printf("файл: %s\nсхема: %d (этот lumend знает %d)\nэкземпляр: %s\nцелостность: ok\n",
		info.Path, info.Version, info.Latest, info.Instance)
}
