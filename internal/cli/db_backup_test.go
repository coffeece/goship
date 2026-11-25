package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

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

// ---- restore ----

const (
	restorePrefix = "/api/v1/orgs/acme/projects/atlas/environments/production/databases/orders-db/backups/" + backupA + "/restore"
	newDBBackups  = "/api/v1/orgs/acme/projects/atlas/environments/production/databases/orders-copy/backups"
	secretPW      = "s3cr3t-p4ssw0rd"
)

const restoreAccepted = `{"database":{"id":"d1","name":"orders-copy","plan":"small","db_name":"orders_copy","db_user":"u","db_password":"` + secretPW + `","status":"provisioning","placement":"goship"},
 "restore":{"id":"r1","backup_id":"` + backupA + `","status":"pending"}}`

func restoreStatus(status, errText string) string {
	return `{"available":true,"backups":[],"restore":{"id":"r1","backup_id":"` + backupA + `","status":"` + status + `","error":"` + errText + `"}}`
}

// restoreAPI answers the listing, the restore request and then the polls with
// the given statuses (the last one repeats). It records the request body and
// the poll paths.
type restoreAPI struct {
	body   map[string]any
	polls  []string
	status []func(w http.ResponseWriter)
}

func serveRestore(t *testing.T, a *restoreAPI, accepted string) {
	t.Helper()
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == dbPrefix+"/backups":
			w.Write([]byte(backupsJSON)) //nolint:errcheck
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/removed-databases"):
			w.Write([]byte(removedJSON)) //nolint:errcheck
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/restore"):
			a.body = map[string]any{}
			json.NewDecoder(r.Body).Decode(&a.body) //nolint:errcheck
			a.polls = append(a.polls, "POST "+r.URL.Path)
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(accepted)) //nolint:errcheck
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/backups"):
			a.polls = append(a.polls, r.URL.Path)
			i := min(len(a.polls)-2, len(a.status)-1)
			a.status[i](w)
		case strings.HasSuffix(r.URL.Path, "/projects") || strings.HasSuffix(r.URL.Path, "/environments"):
			w.Write([]byte(`[]`)) // the lookups that explain a refusal //nolint:errcheck
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	old := restorePollInterval
	restorePollInterval = time.Millisecond
	t.Cleanup(func() { restorePollInterval = old })
}

func body(s string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.Write([]byte(s)) } //nolint:errcheck
}

func code(c int) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(c)
		w.Write([]byte(`{"error":{"code":"x","message":"nope"}}`)) //nolint:errcheck
	}
}

func TestRestoreSendsTheRequestAndWaits(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("running", "")), body(restoreStatus("succeeded", ""))}}
	serveRestore(t, a, restoreAccepted)
	out, errOut, err := runBoth(t, "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy", "--plan", "small")
	if err != nil {
		t.Fatal(err)
	}
	if a.body["name"] != "orders-copy" || a.body["plan"] != "small" {
		t.Errorf("body = %v", a.body)
	}
	if _, ok := a.body["environment"]; ok {
		t.Errorf("environment sent without --to-environment: %v", a.body)
	}
	for _, want := range []string{"Database orders-copy requested on plan small, restoring backup aaaaaaaa of orders-db.", "Restore finished.", "goship db info orders-copy"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, s := range []string{out, errOut} {
		if strings.Contains(s, secretPW) || strings.Contains(s, "db_password") {
			t.Errorf("password leaked:\n%s", s)
		}
	}
	if len(a.polls) != 3 || a.polls[1] != newDBBackups {
		t.Errorf("calls = %v", a.polls)
	}
}

func TestRestoreFromARemovedDatabase(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("succeeded", ""))}}
	serveRestore(t, a, restoreAccepted)
	out, _, err := runBoth(t, "db", "restore", "--removed", "rem-1111-2222", "--backup", "aaaaaaaa", "--name", "orders-copy")
	if err != nil {
		t.Fatal(err)
	}
	if want := "POST /api/v1/orgs/acme/projects/atlas/environments/production/removed-databases/rem-1111-2222/backups/" + backupA + "/restore"; a.polls[0] != want {
		t.Errorf("first call = %q, want %q", a.polls[0], want)
	}
	if !strings.Contains(out, "restoring backup aaaaaaaa of old-db.") {
		t.Errorf("output = %s", out)
	}
}

func TestRestoreNeedsItsFlagsAndExactlyOneSource(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("succeeded", ""))}}
	serveRestore(t, a, restoreAccepted)
	for _, args := range [][]string{
		{"db", "restore", "orders-db", "--name", "x"},
		{"db", "restore", "orders-db", "--backup", "aaaaaaaa"},
		{"db", "restore", "--backup", "aaaaaaaa", "--name", "x"},
		{"db", "restore", "orders-db", "--removed", "rem-1111-2222", "--backup", "aaaaaaaa", "--name", "x"},
	} {
		if _, _, err := runBoth(t, args...); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
	if len(a.polls) != 0 {
		t.Errorf("API called: %v", a.polls)
	}
}

func TestRestoreUnknownBackupIsRefused(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("succeeded", ""))}}
	serveRestore(t, a, restoreAccepted)
	if _, _, err := runBoth(t, "db", "restore", "orders-db", "--backup", "zzzz", "--name", "x"); err == nil {
		t.Fatal("want an error")
	}
	if len(a.polls) != 0 {
		t.Errorf("restore requested: %v", a.polls)
	}
}

func TestRestoreReportsBilling(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("succeeded", ""))}}
	billed := strings.Replace(restoreAccepted, `"placement":"goship"`, `"placement":"goship","billing_starts_at":"2099-01-05T00:00:00Z"`, 1)
	serveRestore(t, a, billed)
	out, _, err := runBoth(t, "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Billing starts when the trial ends on 4 Jan 2099.") {
		t.Errorf("output = %s", out)
	}
}

func TestRestoreToAnotherEnvironmentPollsThere(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("succeeded", ""))}}
	serveRestore(t, a, restoreAccepted)
	_, _, err := runBoth(t, "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy", "--to-environment", "staging")
	if err != nil {
		t.Fatal(err)
	}
	if a.body["environment"] != "staging" {
		t.Errorf("body = %v", a.body)
	}
	if want := "/api/v1/orgs/acme/projects/atlas/environments/staging/databases/orders-copy/backups"; a.polls[1] != want {
		t.Errorf("poll = %q, want %q", a.polls[1], want)
	}
}

func TestRestoreFailedExitsWithTheReason(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("failed", "pg_restore exited 1"))}}
	serveRestore(t, a, restoreAccepted)
	out, _, err := runBoth(t, "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy", "--to-environment", "staging")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"pg_restore exited 1", "orders-copy", "Delete it with `goship db rm orders-copy --environment staging` and restore into a new database."} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if strings.Contains(out+err.Error(), secretPW) {
		t.Error("password leaked")
	}
}

func TestRestoreFailedHintHasNoEnvironmentFlagByDefault(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("failed", "boom"))}}
	serveRestore(t, a, restoreAccepted)
	_, _, err := runBoth(t, "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy")
	if err == nil || !strings.Contains(err.Error(), "Delete it with `goship db rm orders-copy` and restore into a new database.") {
		t.Errorf("err = %v", err)
	}
}

func TestRestoreTimeoutExitsZeroWithTheHint(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("running", ""))}}
	serveRestore(t, a, restoreAccepted)
	out, _, err := runBoth(t, "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy", "--timeout", "50ms")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Still restoring. Check with `goship db backups orders-copy`.") {
		t.Errorf("output = %s", out)
	}
}

func TestRestoreRidesOutThreeTransientFailures(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){code(502), code(503), code(500), body(restoreStatus("succeeded", ""))}}
	serveRestore(t, a, restoreAccepted)
	out, errOut, err := runBoth(t, "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Restore finished.") || !strings.Contains(errOut, "retrying") {
		t.Errorf("out = %s\nerr = %s", out, errOut)
	}
}

func TestRestoreFourTransientFailuresStop(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){code(502)}}
	serveRestore(t, a, restoreAccepted)
	_, _, err := runBoth(t, "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy")
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestRestoreAForbiddenPollStops(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){code(403)}}
	serveRestore(t, a, restoreAccepted)
	_, errOut, err := runBoth(t, "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy")
	if err == nil || strings.Contains(errOut, "retrying") {
		t.Errorf("err = %v, stderr = %s", err, errOut)
	}
	if len(a.polls) != 2 {
		t.Errorf("calls = %v", a.polls)
	}
}

func TestRestoreNoWait(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("running", ""))}}
	serveRestore(t, a, restoreAccepted)
	out, _, err := runBoth(t, "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy", "--no-wait")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Restore started.") || !strings.Contains(out, "orders-copy") || strings.Contains(out, secretPW) {
		t.Errorf("output = %s", out)
	}
	if len(a.polls) != 1 {
		t.Errorf("calls = %v", a.polls)
	}
}

func TestRestoreJSONHasNoPasswordAndDoesNotWait(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("running", ""))}}
	serveRestore(t, a, restoreAccepted)
	out, errOut, err := runBoth(t, "-o", "json", "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out+errOut, secretPW) || strings.Contains(out+errOut, "db_password") {
		t.Errorf("password in output:\n%s\n%s", out, errOut)
	}
	var got struct {
		Database map[string]any `json:"database"`
		Restore  map[string]any `json:"restore"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got.Database["name"] != "orders-copy" || got.Restore["status"] != "pending" {
		t.Errorf("got %v", got)
	}
	if len(a.polls) != 1 {
		t.Errorf("calls = %v", a.polls)
	}
}

func TestRestoreCtrlCPrintsTheHint(t *testing.T) {
	a := &restoreAPI{status: []func(http.ResponseWriter){body(restoreStatus("running", ""))}}
	serveRestore(t, a, restoreAccepted)
	restorePollInterval = 200 * time.Millisecond
	go func() {
		time.Sleep(100 * time.Millisecond)
		syscall.Kill(syscall.Getpid(), syscall.SIGINT) //nolint:errcheck
	}()
	out, _, err := runBoth(t, "db", "restore", "orders-db", "--backup", "aaaaaaaa-1", "--name", "orders-copy")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(out, "Still restoring. Check with `goship db backups orders-copy`.") {
		t.Errorf("output = %s", out)
	}
}

func TestDBBackupsShowsTheRestoreLine(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Replace(backupsJSON, `"backups":[`, `"restore":{"id":"r1","backup_id":"x","status":"failed","error":"disk full"},"backups":[`, 1))) //nolint:errcheck
	})
	out, err := run(t, "", "db", "backups", "orders-db")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Restore into this database: failed") || !strings.Contains(out, "disk full") {
		t.Errorf("output = %s", out)
	}
	if strings.Index(out, "Restore into") > strings.Index(out, "aaaaaaaa") {
		t.Errorf("line is not above the table:\n%s", out)
	}
	// JSON stays the API structure.
	out, err = run(t, "", "-o", "json", "db", "backups", "orders-db")
	if err != nil || strings.Contains(out, "Restore into") {
		t.Errorf("json = %s, %v", out, err)
	}
}
