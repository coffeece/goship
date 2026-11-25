package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
			all, err := c.RemovedDatabases(ctx, t.Org, t.Project, t.Environment)
			if err != nil {
				return err
			}
			for i := range all {
				if all[i].ID == removedID {
					removed = &all[i]
				}
			}
			if removed == nil {
				return errors.New("no removed database with that id")
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
		if b.Status != "succeeded" {
			word := portal.DatabaseBackups{Backups: []portal.DatabaseBackup{b}}.Rows()[0].Status
			return fmt.Errorf("backup %s is %s: only completed backups can be downloaded", b.ID[:min(8, len(b.ID))], word)
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
