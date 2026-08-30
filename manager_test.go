package main

import (
	"archive/zip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOSReleaseValue(t *testing.T) {
	content := "NAME=Ubuntu\nPRETTY_NAME=\"Ubuntu 24.04.4 LTS\"\n"
	if got := osReleaseValue(content, `PRETTY_NAME`); got != `Ubuntu 24.04.4 LTS` {
		t.Fatalf("got %q", got)
	}
}

func TestBundledRootCAs(t *testing.T) {
	client, err := newHTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatal(`HTTP client does not contain a TLS root CA pool`)
	}
	if len(transport.TLSClientConfig.RootCAs.Subjects()) == 0 {
		t.Fatal(`TLS root CA pool is empty`)
	}
}

func TestFindRootFSArchive(t *testing.T) {
	dir := t.TempDir()
	sdcard := filepath.Join(dir, `SDCARD`)
	if err := os.Mkdir(sdcard, 0755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(sdcard, `ubuntu-base-24.04-base-arm64.tar.gz`)
	if err := os.WriteFile(want, nil, 0644); err != nil {
		t.Fatal(err)
	}
	m := Manager{AppDir: dir, RootFS: filepath.Join(dir, `root`), SDCard: sdcard}
	if got := m.findRootFSArchive(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInstallPackagesOutsideSDCardRootAreIgnored(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, `payload`)
	if err := os.Mkdir(payload, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, ubuntuArchiveName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, `trimui.portmaster.zip`), nil, 0644); err != nil {
		t.Fatal(err)
	}
	m := Manager{AppDir: dir, RootFS: filepath.Join(dir, `root`), SDCard: filepath.Join(dir, `SDCARD`)}
	if got := m.findRootFSArchive(); got != `` {
		t.Fatalf(`found rootfs outside SD-card root: %s`, got)
	}
	if got := m.findPortMasterZIP(); got != `` {
		t.Fatalf(`found PortMaster outside SD-card root: %s`, got)
	}
}

func TestFreshInstallRequiresArchivesEvenWhenEnvironmentExists(t *testing.T) {
	dir := t.TempDir()
	rootfs := filepath.Join(dir, `ubuntu`)
	sdcard := filepath.Join(dir, `SDCARD`)
	if err := os.MkdirAll(filepath.Join(rootfs, `etc`), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootfs, `etc`, `os-release`), []byte("PRETTY_NAME=Ubuntu\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sdcard, `Apps`, `PortMaster`, `PortMaster`), 0755); err != nil {
		t.Fatal(err)
	}
	m := Manager{AppDir: dir, RootFS: rootfs, SDCard: sdcard}
	missing := m.MissingDownloads()
	if len(missing) != 2 {
		t.Fatalf(`fresh install missing downloads = %d, want 2: %+v`, len(missing), missing)
	}
	for _, spec := range missing {
		if filepath.Dir(spec.Destination) != sdcard {
			t.Fatalf(`download %s destination = %s, want SD-card root %s`, spec.Name, spec.Destination, sdcard)
		}
	}
}

func TestExtractZIPRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{`../escape`, `/absolute`, `dir/../../escape`} {
		if _, err := archiveTarget(root, name); err == nil {
			t.Fatalf("accepted unsafe path %q", name)
		}
	}
}

func TestRemoveInstallTreeOnlyDeletesExactExpectedChild(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, `ubuntu`)
	if err := os.MkdirAll(filepath.Join(target, `etc`), 0755); err != nil {
		t.Fatal(err)
	}
	if err := removeInstallTree(target, parent, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf(`target still exists: %v`, err)
	}
	if err := removeInstallTree(parent, parent, parent); err == nil {
		t.Fatal(`accepted deletion of the parent directory`)
	}
}

func TestArchiveTargetAcceptsChild(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, `Apps`, `PortMaster`, `launch.sh`)
	got, err := archiveTarget(root, `Apps/PortMaster/launch.sh`)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestOfficialPortMasterLayoutInstallsToCustomAppName(t *testing.T) {
	workspace := t.TempDir()
	archive := filepath.Join(workspace, `trimui.portmaster.zip`)
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create(`Apps/PortMaster/PortMaster/version`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(entry, `test`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	sdcard := filepath.Join(workspace, `SDCARD`)
	destination := filepath.Join(sdcard, `Apps`, `CustomPortMasterRuntime`)
	if err := installPortMasterZIP(archive, sdcard, destination); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(destination, `PortMaster`, `version`)
	if data, err := os.ReadFile(want); err != nil || string(data) != `test` {
		t.Fatalf(`official layout not extracted at %s: data=%q err=%v`, want, data, err)
	}
	for _, unwanted := range []string{
		filepath.Join(sdcard, `Apps`, `PortMaster`),
		filepath.Join(sdcard, `Apps`, `Apps`, `PortMaster`),
	} {
		if _, err := os.Stat(unwanted); !os.IsNotExist(err) {
			t.Fatalf(`conflicting directory was created at %s: %v`, unwanted, err)
		}
	}
}

func TestDownloadFileReportsKnownSizeAndCommitsAtomically(t *testing.T) {
	content := strings.Repeat(`PortMaster`, 4096)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set(`Content-Length`, strconv.Itoa(len(content)))
		_, _ = io.WriteString(response, content)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), `payload`, `package.zip`)
	var updates []DownloadProgress
	err := downloadFile(server.Client(), DownloadSpec{
		Name:        `test package`,
		URL:         server.URL,
		Destination: destination,
	}, func(progress DownloadProgress) {
		updates = append(updates, progress)
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf(`downloaded content mismatch: got %d bytes, want %d`, len(data), len(content))
	}
	if _, err := os.Stat(destination + `.part`); !os.IsNotExist(err) {
		t.Fatalf(`partial file remains: %v`, err)
	}
	last := updates[len(updates)-1]
	if last.Downloaded != int64(len(content)) || last.Total != int64(len(content)) {
		t.Fatalf(`last progress = %+v`, last)
	}
}

func TestDownloadFileWithoutSizeReportsDownloadedBytes(t *testing.T) {
	content := strings.Repeat(`x`, 1024*1024+17)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		flusher := response.(http.Flusher)
		_, _ = io.WriteString(response, content[:1])
		flusher.Flush()
		_, _ = io.WriteString(response, content[1:])
	}))
	defer server.Close()

	var last DownloadProgress
	destination := filepath.Join(t.TempDir(), `package.tar.gz`)
	err := downloadFile(server.Client(), DownloadSpec{Name: `rootfs`, URL: server.URL, Destination: destination}, func(progress DownloadProgress) {
		last = progress
	})
	if err != nil {
		t.Fatal(err)
	}
	if last.Total != -1 || last.Downloaded != int64(len(content)) {
		t.Fatalf(`last progress = %+v`, last)
	}
}

func TestDownloadFileHTTPErrorRemovesPartial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Error(response, `no package`, http.StatusBadGateway)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), `package.zip`)
	err := downloadFile(server.Client(), DownloadSpec{Name: `broken`, URL: server.URL, Destination: destination}, nil)
	if err == nil || !strings.Contains(err.Error(), `502`) {
		t.Fatalf(`unexpected error: %v`, err)
	}
	for _, path := range []string{destination, destination + `.part`} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf(`unexpected file %s: %v`, path, statErr)
		}
	}
}
