package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

func newDBBackupsCmd(app *App) *cobra.Command {
	var removed bool
	cmd := &cobra.Command{
		Use:   "backups [<database>] [--removed]",
		Short: "List a database's daily backups, or those of removed databases",
		Args:  cobra.MaximumNArgs(1),
	}
	cmd.Flags().BoolVar(&removed, "removed", false, "list the backups of removed databases instead")
	cmd.RunE = readTargetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if removed == (len(args) == 1) {
			return errors.New("give a database name, or --removed, but not both")
		}
		if removed {
			return listRemovedBackups(cmd, app, t)
		}
		list, err := app.Portal().DatabaseBackups(cmd.Context(), t.Org, t.Project, t.Environment, args[0])
		if err != nil {
			return err
		}
		if app.Global.Output == render.JSON {
			return app.Renderer().Render(list)
		}
		if r := list.Restore; r != nil {
			if err := app.Renderer().Message("Restore into this database: %s", r.Status); err != nil {
				return err
			}
			if r.Status == "failed" && r.Error != "" {
				if err := app.Renderer().Message("%s", r.Error); err != nil {
					return err
				}
			}
		}
		switch {
		case !list.Available:
			return app.Renderer().Message("%s", unavailableMessage(list.Reason))
		case len(list.Backups) == 0:
			return app.Renderer().Message("No backups yet. The first one is taken tonight.")
		}
		return app.Renderer().Render(list.Rows())
	})
	return cmd
}

func unavailableMessage(reason string) string {
	if reason == "own_server" {
		return "This database runs on your own server: use its point-in-time recovery."
	}
	return "Backups are not available yet."
}

func listRemovedBackups(cmd *cobra.Command, app *App, t Target) error {
	removed, err := app.Portal().RemovedDatabases(cmd.Context(), t.Org, t.Project, t.Environment)
	if err != nil {
		return err
	}
	if app.Global.Output == render.JSON {
		if removed == nil {
			removed = []portal.RemovedDatabase{}
		}
		return app.Renderer().Render(removed)
	}
	if len(removed) == 0 {
		return app.Renderer().Message("No removed databases with backups.")
	}
	r := app.Renderer()
	const day = "2006-01-02"
	for i, d := range removed {
		if i > 0 {
			if err := r.Message(""); err != nil {
				return err
			}
		}
		if err := r.Message("%s  %s", d.ID, d.Name); err != nil {
			return err
		}
		if err := r.Message("Removed %s; restore or download until %s.", d.RemovedAt.UTC().Format(day), d.AvailableUntil.UTC().Format(day)); err != nil {
			return err
		}
		if len(d.Backups) == 0 {
			err = r.Message("No backups kept.")
		} else {
			err = r.Render(d.Rows())
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// resolveBackupID finds the backup the customer means by the full id or the
// 8 characters the table shows.
func resolveBackupID(list []portal.DatabaseBackup, given string) (portal.DatabaseBackup, error) {
	var found []portal.DatabaseBackup
	if given != "" {
		for _, b := range list {
			if strings.HasPrefix(b.ID, given) {
				found = append(found, b)
			}
		}
	}
	switch len(found) {
	case 0:
		return portal.DatabaseBackup{}, errors.New("no backup with that id")
	case 1:
		return found[0], nil
	}
	return portal.DatabaseBackup{}, fmt.Errorf("%q matches %d backups, give more characters", given, len(found))
}

// requireCompleted refuses a backup that is not a finished one, naming its
// status with the word the table uses. what is the verb: "downloaded", "restored".
func requireCompleted(b portal.DatabaseBackup, what string) error {
	if b.Status == "succeeded" {
		return nil
	}
	word := portal.DatabaseBackups{Backups: []portal.DatabaseBackup{b}}.Rows()[0].Status
	return fmt.Errorf("backup %s is %s: only completed backups can be %s", b.ID[:min(8, len(b.ID))], word, what)
}

// findRemoved is the removed database with that id, among those of the target.
func findRemoved(ctx context.Context, c *portal.Client, t Target, id string) (*portal.RemovedDatabase, error) {
	all, err := c.RemovedDatabases(ctx, t.Org, t.Project, t.Environment)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, errors.New("no removed database with that id")
}

// defaultDumpName is where a download goes when --file is not given. The
// database name is reduced to its last path element so nothing an argument
// says can put the file outside the current directory.
func defaultDumpName(database, day string) string {
	return filepath.Base(database+"-"+filepath.Base(day)) + ".dump"
}

func newDBBackupDownloadCmd(app *App) *cobra.Command {
	var removedID, file string
	var force bool
	cmd := &cobra.Command{
		Use:   "backup-download [<database>] <backup-id> [--removed <id>] [--file <path>] [--force]",
		Short: "Download a backup as a pg_dump file you can restore with pg_restore",
		Args: func(cmd *cobra.Command, args []string) error {
			if removedID != "" {
				return cobra.ExactArgs(1)(cmd, args)
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
	}
	f := cmd.Flags()
	f.StringVar(&removedID, "removed", "", "id of a removed database; then give only the backup id")
	f.StringVarP(&file, "file", "f", "", "where to write the dump (default <database>-<day>.dump; - for stdout)")
	f.BoolVar(&force, "force", false, "replace the file if it exists")

	cmd.RunE = readTargetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if err := noJSON(app, cmd); err != nil {
			return err
		}
		ctx, stop := interruptible(cmd.Context())
		defer stop()
		cmd.SetContext(ctx)
		c := app.Portal()

		var (
			name    string
			backups []portal.DatabaseBackup
			removed *portal.RemovedDatabase
		)
		if removedID != "" {
			var err error
			if removed, err = findRemoved(ctx, c, t, removedID); err != nil {
				return err
			}
			name, backups = removed.Name, removed.Backups
		} else {
			list, err := c.DatabaseBackups(ctx, t.Org, t.Project, t.Environment, args[0])
			if err != nil {
				return err
			}
			if !list.Available {
				return errors.New(unavailableMessage(list.Reason))
			}
			name, backups = args[0], list.Backups
		}
		b, err := resolveBackupID(backups, args[len(args)-1])
		if err != nil {
			return err
		}
		if err := requireCompleted(b, "downloaded"); err != nil {
			return err
		}

		toStdout := file == "-"
		if file == "" {
			file = defaultDumpName(name, b.Day)
		}
		if !toStdout && !force {
			if _, err := os.Lstat(file); err == nil {
				return fmt.Errorf("%s already exists; pass --force to replace it or --file to choose another name", file)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}

		var dl *portal.BackupDownload
		if removed != nil {
			dl, err = c.RemovedDatabaseBackupDownload(ctx, t.Org, t.Project, t.Environment, removed.ID, b.ID)
		} else {
			dl, err = c.DatabaseBackupDownload(ctx, t.Org, t.Project, t.Environment, name, b.ID)
		}
		if err != nil {
			return err
		}

		errw := cmd.ErrOrStderr()
		if toStdout {
			fmt.Fprint(errw, "Downloading to standard output ... ")
			n, err := portal.FetchSignedLink(ctx, dl.URL, cmd.OutOrStdout())
			if err != nil {
				fmt.Fprintln(errw)
				return err
			}
			fmt.Fprintf(errw, "%s\nRestore it yourself with: pg_restore --no-owner -d <connection string>  (the dump is on stdout: pipe it in)\n", portal.FormatSize(n))
			return nil
		}
		fmt.Fprintf(errw, "Downloading to %s ... ", file)
		n, err := downloadToFile(ctx, dl.URL, file, force)
		if err != nil {
			fmt.Fprintln(errw)
			return err
		}
		fmt.Fprintf(errw, "%s\nRestore it yourself with: pg_restore --no-owner -d <connection string> %s\n", portal.FormatSize(n), file)
		return nil
	})
	return cmd
}

// downloadToFile writes the dump to a temporary file next to target (mode
// 0600, unpredictable name) and moves it onto target only once every byte has
// arrived. The temporary file is removed on every failure path, Ctrl-C included.
func downloadToFile(ctx context.Context, link, target string, force bool) (int64, error) {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".goship-dump-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // gone already after a successful move

	n, err := portal.FetchSignedLink(ctx, link, tmp)
	if err != nil {
		tmp.Close() //nolint:errcheck
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if force {
		return n, os.Rename(tmp.Name(), target)
	}
	// A hard link fails if target appeared meanwhile, so a file created during
	// the download is never replaced; the temporary name is removed after.
	err = os.Link(tmp.Name(), target)
	switch {
	case err == nil:
		return n, nil
	case errors.Is(err, fs.ErrExist):
		return 0, fmt.Errorf("%s was created while downloading; pass --force to replace it", target)
	}
	// No hard links here (some removable file systems): check again and move.
	if _, serr := os.Lstat(target); serr == nil {
		return 0, fmt.Errorf("%s was created while downloading; pass --force to replace it", target)
	}
	return n, os.Rename(tmp.Name(), target)
}

// restorePollInterval is how often a restore is looked at while waiting.
var restorePollInterval = 5 * time.Second

func newDBRestoreCmd(app *App) *cobra.Command {
	var backup, name, removedID, plan, toEnv string
	var noWait bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "restore [<database>] --backup <id> --name <new> [--removed <id>] [--plan <plan>] [--to-environment <env>] [--no-wait] [--timeout 15m]",
		Short: "Restore a backup into a new database",
		Args:  cobra.MaximumNArgs(1),
	}
	f := cmd.Flags()
	f.StringVar(&backup, "backup", "", "backup to restore: the id from `goship db backups` (required)")
	f.StringVar(&name, "name", "", "name of the new database (required)")
	f.StringVar(&removedID, "removed", "", "restore from a removed database (its id) instead of <database>")
	f.StringVar(&plan, "plan", "", "plan of the new database (default: the plan of the source)")
	f.StringVar(&toEnv, "to-environment", "", "environment the new database goes in (default: the current one)")
	f.BoolVar(&noWait, "no-wait", false, "return once the restore has started")
	f.DurationVar(&timeout, "timeout", 15*time.Minute, "how long to wait for the restore")
	_ = cmd.MarkFlagRequired("backup")
	_ = cmd.MarkFlagRequired("name")

	cmd.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if (removedID == "") == (len(args) == 0) {
			return errors.New("give a database name, or --removed, but not both")
		}
		ctx, stop := interruptible(cmd.Context())
		defer stop()
		cmd.SetContext(ctx)
		c := app.Portal()

		var (
			source  string
			backups []portal.DatabaseBackup
			removed *portal.RemovedDatabase
		)
		if removedID != "" {
			var err error
			if removed, err = findRemoved(ctx, c, t, removedID); err != nil {
				return err
			}
			source, backups = removed.Name, removed.Backups
		} else {
			list, err := c.DatabaseBackups(ctx, t.Org, t.Project, t.Environment, args[0])
			if err != nil {
				return err
			}
			if !list.Available {
				return errors.New(unavailableMessage(list.Reason))
			}
			source, backups = args[0], list.Backups
		}
		b, err := resolveBackupID(backups, backup)
		if err != nil {
			return err
		}
		if err := requireCompleted(b, "restored"); err != nil {
			return err
		}

		req := portal.RestoreRequest{Name: name, Plan: plan, Environment: toEnv}
		var res *portal.RestoreResult
		if removed != nil {
			res, err = c.RestoreRemovedDatabaseBackup(ctx, t.Org, t.Project, t.Environment, removed.ID, b.ID, req)
		} else {
			res, err = c.RestoreDatabaseBackup(ctx, t.Org, t.Project, t.Environment, source, b.ID, req)
		}
		if err != nil {
			return err
		}
		if app.Global.Output == render.JSON {
			return renderRestoreWithoutPassword(app, res)
		}

		short := b.ID[:min(8, len(b.ID))]
		if p := res.Database.Plan; p != "" {
			err = reportChange(app, portal.Change{BillingStartsAt: res.Database.BillingStartsAt}, "Database %s requested on plan %s, restoring backup %s of %s.", name, p, short, source)
		} else {
			err = reportChange(app, portal.Change{BillingStartsAt: res.Database.BillingStartsAt}, "Database %s requested, restoring backup %s of %s.", name, short, source)
		}
		if err != nil {
			return err
		}

		// Commands that act on the new database, pasteable from anywhere.
		// The environment is named only when the user named one on the command line.
		flag := ""
		if hintEnv := cmp.Or(toEnv, app.Global.Environment); hintEnv != "" {
			flag = " --environment " + hintEnv
		}
		infoLine := fmt.Sprintf("`goship db info %s%s` shows its state; `goship db bind %s --app <app>%s` connects an app.", name, flag, name, flag)
		r := app.Renderer()
		if noWait {
			if err := r.Message("Restore started."); err != nil {
				return err
			}
			return r.Message("%s", infoLine)
		}
		pollEnv := cmp.Or(toEnv, t.Environment)
		stillRestoring := func() error {
			return r.Message("Still restoring. Check with `goship db backups %s%s`.", name, flag)
		}
		err = waitForRestore(cmd, c, t, pollEnv, name, timeout)
		switch {
		case err == nil:
			if err := r.Message("Restore finished."); err != nil {
				return err
			}
			return r.Message("%s", infoLine)
		case errors.Is(err, errRestoreTimeout):
			return stillRestoring()
		case ctx.Err() != nil:
			if merr := stillRestoring(); merr != nil {
				return merr
			}
			return ctx.Err()
		case errors.Is(err, errRestoreFailed):
			hint := fmt.Sprintf("Delete it with `goship db rm %s%s` and restore into a new database.", name, flag)
			return fmt.Errorf("%w\n%s", err, hint)
		}
		// The API gave up on us; the restore is still running on the server.
		if merr := stillRestoring(); merr != nil {
			return merr
		}
		return err
	})
	return cmd
}

var (
	errRestoreTimeout = errors.New("restore wait timed out")
	errRestoreFailed  = errors.New("restore failed")
)

// waitForRestore polls the new database until its restore ends: nil when it
// succeeded, errRestoreFailed (wrapped with the reason) when it failed,
// errRestoreTimeout at the deadline. Any other status counts as in progress.
func waitForRestore(cmd *cobra.Command, c *portal.Client, t Target, env, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	retrier := &pollRetrier{errw: cmd.ErrOrStderr()}
	for {
		list, err := c.DatabaseBackups(ctx, t.Org, t.Project, env, name)
		switch {
		case err != nil && cmd.Context().Err() != nil:
			return err
		case err != nil && ctx.Err() != nil:
			return errRestoreTimeout
		case err != nil && retrier.retry(err):
		case err != nil:
			return err
		default:
			retrier.ok()
			if r := list.Restore; r != nil {
				switch r.Status {
				case "succeeded":
					return nil
				case "failed":
					return fmt.Errorf("%w for database %s: %s", errRestoreFailed, name, cmp.Or(r.Error, "no reason given"))
				}
			}
		}
		select {
		case <-ctx.Done():
			if cmd.Context().Err() != nil {
				return cmd.Context().Err()
			}
			return errRestoreTimeout
		case <-time.After(restorePollInterval):
		}
	}
}

// renderRestoreWithoutPassword prints the API's answer with database.db_password
// removed altogether.
func renderRestoreWithoutPassword(app *App, res *portal.RestoreResult) error {
	raw, err := json.Marshal(res)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if db, ok := doc["database"].(map[string]any); ok {
		delete(db, "db_password")
	}
	return app.Renderer().Render(doc)
}
