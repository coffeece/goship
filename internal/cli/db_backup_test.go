package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/portal"
)

const (
	backupA = "aaaaaaaa-1111-4000-8000-000000000001"
	backupB = "aaaaaaaa-2222-4000-8000-000000000002"
	backupC = "cccccccc-3333-4000-8000-000000000003"
)

const dbPrefix = "/api/v1/orgs/acme/projects/atlas/environments/production/databases/orders-db"

const backupsJSON = `{"available":true,"backups":[
 {"id":"` + backupC + `","day":"2026-11-24","finished_at":"2026-11-24T03:10:00Z","size_bytes":13002342,"status":"succeeded","kind":"daily"},
 {"id":"` + backupA + `","day":"2026-11-23","finished_at":"2026-11-23T03:10:00Z","size_bytes":100,"status":"succeeded","kind":"pause"},
 {"id":"` + backupB + `","day":"2026-11-25","status":"pending","kind":"daily"}]}`

// runBoth is run with stderr captured too.
func runBoth(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut strings.Builder
	root := NewRoot("test")
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(strings.NewReader(""))
	err := root.Execute()
	return out.String(), errOut.String(), err
}

func inTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) }) //nolint:errcheck
	return dir
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestDBBackupsShowsTheTable(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != dbPrefix+"/backups" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(backupsJSON)) //nolint:errcheck
	})
	out, err := run(t, "", "db", "backups", "orders-db")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"aaaaaaaa", "2026-11-24", "12.4 MB", "completed", "completed (on pause)", "scheduled"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, backupA) {
		t.Errorf("the table shows a full id:\n%s", out)
	}
}

func TestDBBackupsNoDataLines(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"available":false,"reason":"own_server","backups":[]}`, "This database runs on your own server: use its point-in-time recovery."},
		{`{"available":false,"reason":"not_enabled","backups":[]}`, "Backups are not available yet."},
		{`{"available":true,"backups":[]}`, "No backups yet. The first one is taken tonight."},
	} {
		stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(tc.body)) }) //nolint:errcheck
		out, err := run(t, "", "db", "backups", "orders-db")
		if err != nil {
			t.Fatalf("%s: %v", tc.want, err)
		}
		if strings.TrimSpace(out) != tc.want {
			t.Errorf("output = %q, want %q", out, tc.want)
		}
	}
}

func TestDBBackupsJSONIsTheAPIStructure(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"available":false,"reason":"own_server","backups":[]}`)) //nolint:errcheck
	})
	out, err := run(t, "", "db", "backups", "orders-db", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"available": false`) || !strings.Contains(out, `"own_server"`) {
		t.Errorf("output = %s", out)
	}
}

const removedJSON = `{"removed_databases":[{"id":"rem-1111-2222","name":"old-db","plan":"small","removed_at":"2026-11-20T10:00:00Z","available_until":"2026-12-20T10:00:00Z","backups":[
 {"id":"` + backupA + `","day":"2026-11-19","finished_at":"2026-11-19T03:10:00Z","size_bytes":2048,"status":"succeeded","kind":"daily"}]}]}`

func TestDBBackupsRemoved(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/removed-databases") {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(removedJSON)) //nolint:errcheck
	})
	out, err := run(t, "", "db", "backups", "--removed")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"rem-1111-2222", "old-db", "2026-11-20", "restore or download until 2026-12-20", "2026-11-19", "2.0 KB"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestDBBackupsRemovedNone(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"removed_databases":[]}`)) }) //nolint:errcheck
	out, err := run(t, "", "db", "backups", "--removed")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "No removed databases with backups." {
		t.Errorf("output = %q", out)
	}
	out, err = run(t, "", "db", "backups", "--removed", "-o", "json")
	if err != nil || !strings.Contains(out, "[]") {
		t.Errorf("json = %q, %v", out, err)
	}
}

func TestDBBackupsNeedsExactlyOneTarget(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) { t.Error("the API was called") })
	if _, err := run(t, "", "db", "backups"); err == nil {
		t.Error("neither a database nor --removed was accepted")
	}
	if _, err := run(t, "", "db", "backups", "orders-db", "--removed"); err == nil {
		t.Error("both a database and --removed were accepted")
	}
}

func TestResolveBackupID(t *testing.T) {
	list := []portal.DatabaseBackup{{ID: backupA}, {ID: backupB}, {ID: backupC}}
	got, err := resolveBackupID(list, "cccccccc")
	if err != nil || got.ID != backupC {
		t.Errorf("unique prefix: %v, %v", got.ID, err)
	}
	got, err = resolveBackupID(list, backupA)
	if err != nil || got.ID != backupA {
		t.Errorf("full id: %v, %v", got.ID, err)
	}
	if _, err = resolveBackupID(list, "aaaaaaaa"); err == nil || !strings.Contains(err.Error(), "matches 2 backups, give more characters") {
		t.Errorf("ambiguous: %v", err)
	}
	if _, err = resolveBackupID(list, "zzzz"); err == nil || !strings.Contains(err.Error(), "no backup with that id") {
		t.Errorf("none: %v", err)
	}
	if _, err = resolveBackupID(list, ""); err == nil {
		t.Error("empty id accepted")
	}
}

// storageAPI serves the listing, the link, and (on its own TLS server) the dump.
func storageAPI(t *testing.T, dump func(w http.ResponseWriter, r *http.Request)) (link string) {
	t.Helper()
	storage := httptest.NewTLSServer(http.HandlerFunc(dump))
	t.Cleanup(storage.Close)
	t.Cleanup(portal.SetStorageTLSForTest(storage.Client().Transport.(*http.Transport).TLSClientConfig))
	link = storage.URL + "/private/dump?sig=s3cret-signature"
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case dbPrefix + "/backups":
			w.Write([]byte(backupsJSON)) //nolint:errcheck
		case dbPrefix + "/backups/" + backupC + "/download", dbPrefix + "/backups/" + backupA + "/download":
			w.Write([]byte(`{"url":"` + link + `","expires_at":"2026-11-25T12:00:00Z"}`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/atlas/environments/production/removed-databases":
			w.Write([]byte(removedJSON)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/atlas/environments/production/removed-databases/rem-1111-2222/backups/" + backupA + "/download":
			w.Write([]byte(`{"url":"` + link + `","expires_at":"2026-11-25T12:00:00Z"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected API call %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return link
}

func serveDump(w http.ResponseWriter, r *http.Request) { w.Write([]byte("PGDMP-data")) } //nolint:errcheck

func TestBackupDownloadWritesTheFile(t *testing.T) {
	link := storageAPI(t, serveDump)
	dir := inTempDir(t)
	out, errOut, err := runBoth(t, "db", "backup-download", "orders-db", "cccccccc")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "orders-db-2026-11-24.dump"))
	if err != nil || string(got) != "PGDMP-data" {
		t.Fatalf("file = %q, %v", got, err)
	}
	if names := dirNames(t, dir); len(names) != 1 {
		t.Errorf("directory holds %v", names)
	}
	info, _ := os.Stat(filepath.Join(dir, "orders-db-2026-11-24.dump"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
	if !strings.Contains(errOut, "Downloading to orders-db-2026-11-24.dump ... 10 B") ||
		!strings.Contains(errOut, "Restore it yourself with: pg_restore --no-owner -d <connection string> orders-db-2026-11-24.dump") {
		t.Errorf("stderr = %q", errOut)
	}
	if strings.Contains(out+errOut, "s3cret") || strings.Contains(out+errOut, link) {
		t.Error("the link was printed")
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
}

func TestBackupDownloadRefusesExistingUnlessForce(t *testing.T) {
	storageAPI(t, serveDump)
	dir := inTempDir(t)
	target := filepath.Join(dir, "mine.dump")
	os.WriteFile(target, []byte("precious"), 0o644) //nolint:errcheck
	if _, _, err := runBoth(t, "db", "backup-download", "orders-db", "cccccccc", "--file", "mine.dump"); err == nil {
		t.Fatal("an existing file was overwritten")
	}
	if b, _ := os.ReadFile(target); string(b) != "precious" {
		t.Errorf("target = %q", b)
	}
	if _, _, err := runBoth(t, "db", "backup-download", "orders-db", "cccccccc", "--file", "mine.dump", "--force"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "PGDMP-data" {
		t.Errorf("target = %q", b)
	}
	if names := dirNames(t, dir); len(names) != 1 {
		t.Errorf("directory holds %v", names)
	}
}

func TestBackupDownloadExistingTargetIsCheckedBeforeAskingForALink(t *testing.T) {
	linkAsked := false
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/download") {
			linkAsked = true
		}
		w.Write([]byte(backupsJSON)) //nolint:errcheck
	})
	dir := inTempDir(t)
	os.WriteFile(filepath.Join(dir, "orders-db-2026-11-24.dump"), []byte("x"), 0o644) //nolint:errcheck
	if _, _, err := runBoth(t, "db", "backup-download", "orders-db", "cccccccc"); err == nil || linkAsked {
		t.Errorf("err = %v, link asked = %v", err, linkAsked)
	}
}

func TestBackupDownloadFailedTransferLeavesNothing(t *testing.T) {
	storageAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("partial")) //nolint:errcheck
	})
	dir := inTempDir(t)
	_, errOut, err := runBoth(t, "db", "backup-download", "orders-db", "cccccccc")
	if err == nil {
		t.Fatal("expected a failure")
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Errorf("directory holds %v", names)
	}
	if strings.Contains(err.Error()+errOut, "s3cret") {
		t.Error("the link leaked")
	}
}

func TestBackupDownloadFailedTransferKeepsAnOlderFileWithForce(t *testing.T) {
	storageAPI(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })
	dir := inTempDir(t)
	os.WriteFile(filepath.Join(dir, "old.dump"), []byte("old"), 0o644) //nolint:errcheck
	if _, _, err := runBoth(t, "db", "backup-download", "orders-db", "cccccccc", "-f", "old.dump", "--force"); err == nil {
		t.Fatal("expected a failure")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "old.dump")); string(b) != "old" {
		t.Errorf("old file = %q", b)
	}
	if names := dirNames(t, dir); len(names) != 1 {
		t.Errorf("directory holds %v", names)
	}
}

func TestBackupDownloadToStdout(t *testing.T) {
	storageAPI(t, serveDump)
	dir := inTempDir(t)
	out, errOut, err := runBoth(t, "db", "backup-download", "orders-db", "cccccccc", "--file", "-")
	if err != nil {
		t.Fatal(err)
	}
	if out != "PGDMP-data" {
		t.Errorf("stdout = %q", out)
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Errorf("directory holds %v", names)
	}
	if !strings.Contains(errOut, "Downloading") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestBackupDownloadRefusesJSON(t *testing.T) {
	storageAPI(t, serveDump)
	inTempDir(t)
	if _, _, err := runBoth(t, "db", "backup-download", "orders-db", "cccccccc", "-o", "json"); err == nil {
		t.Error("-o json accepted")
	}
}

func TestBackupDownloadOnlyCompletedBackups(t *testing.T) {
	storageAPI(t, serveDump)
	dir := inTempDir(t)
	_, _, err := runBoth(t, "db", "backup-download", "orders-db", backupB)
	if err == nil || !strings.Contains(err.Error(), "scheduled") {
		t.Errorf("err = %v", err)
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Errorf("directory holds %v", names)
	}
}

func TestBackupDownloadRemoved(t *testing.T) {
	storageAPI(t, serveDump)
	dir := inTempDir(t)
	if _, _, err := runBoth(t, "db", "backup-download", "--removed", "rem-1111-2222", "aaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "old-db-2026-11-19.dump")); err != nil || string(b) != "PGDMP-data" {
		t.Errorf("file = %q, %v", b, err)
	}
	_, _, err := runBoth(t, "db", "backup-download", "--removed", "nope", "aaaaaaaa")
	if err == nil || !strings.Contains(err.Error(), "no removed database with that id") {
		t.Errorf("err = %v", err)
	}
}

func TestBackupDownloadDefaultNameStaysInTheCurrentDirectory(t *testing.T) {
	storageAPI(t, serveDump)
	dir := inTempDir(t)
	// A database name with path parts must not move the file.
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/download") {
			w.Write([]byte(`{"url":"https://127.0.0.1:1/x","expires_at":"2026-11-25T12:00:00Z"}`)) //nolint:errcheck
			return
		}
		w.Write([]byte(backupsJSON)) //nolint:errcheck
	})
	_, _, _ = runBoth(t, "db", "backup-download", "../../evil", "cccccccc")
	if _, err := os.Stat(filepath.Join(dir, "..", "evil-2026-11-24.dump")); err == nil {
		t.Error("a file was written outside the current directory")
	}
	if got := defaultDumpName("../../evil", "2026-11-24"); got != "evil-2026-11-24.dump" {
		t.Errorf("name = %q", got)
	}
}

func TestBackupDownloadVerboseNeverShowsTheLink(t *testing.T) {
	link := storageAPI(t, serveDump)
	inTempDir(t)
	r, w, _ := os.Pipe()
	orig := os.Stderr
	os.Stderr = w
	out, errOut, err := runBoth(t, "db", "backup-download", "orders-db", "cccccccc", "--verbose")
	os.Stderr = orig
	w.Close() //nolint:errcheck
	trace := new(strings.Builder)
	buf := make([]byte, 4096)
	for {
		n, rerr := r.Read(buf)
		trace.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(trace.String(), "/download") {
		t.Errorf("the trace shows no API request: %q", trace.String())
	}
	for _, s := range []string{out, errOut, trace.String()} {
		if strings.Contains(s, "s3cret") || strings.Contains(s, link) {
			t.Errorf("the link appears in %q", s)
		}
	}
}

func TestDownloadToFileRemovesTheTempFileOnCancel(t *testing.T) {
	started := make(chan struct{})
	storage := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("some")) //nolint:errcheck
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer storage.Close()
	t.Cleanup(portal.SetStorageTLSForTest(storage.Client().Transport.(*http.Transport).TLSClientConfig))
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	if _, err := downloadToFile(ctx, storage.URL+"/x", filepath.Join(dir, "out.dump"), false); err == nil {
		t.Fatal("expected the cancellation to fail the download")
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Errorf("directory holds %v", names)
	}
}
