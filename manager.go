package main

import (
	"archive/zip"
	"bufio"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// Change these two names to build a differently namespaced installation.
	rootFSDirName        = `ubuntu-portmaster-rootfs`
	portMasterAppDirName = `UbuntuPortMasterRuntime`
	defaultSDCard        = `/mnt/SDCARD`
	defaultRootFS        = `/mnt/UDISK/` + rootFSDirName
	defaultPortMasterApp = defaultSDCard + `/Apps/` + portMasterAppDirName
	// portMasterURL     = `https://github.com/PortsMaster/PortMaster-GUI/releases/download/2026.07.28-1212/trimui.portmaster.zip`
	// ubuntuRootfsURL   = `https://cdimage.ubuntu.com/ubuntu-base/releases/24.04.4/release/ubuntu-base-24.04.4-base-arm64.tar.gz`
	portMasterURL     = `http://192.168.10.124:4637/trimui.portmaster.zip`
	ubuntuRootfsURL   = `http://192.168.10.124:4637/ubuntu-base-24.04.4-base-arm64.tar.gz`
	ubuntuArchiveName = `ubuntu-base-24.04.4-base-arm64.tar.gz`
)

//go:embed assets/cacert.pem
var bundledRootCAs []byte

type Manager struct {
	AppDir        string
	RootFS        string
	SDCard        string
	PortMasterApp string
	Client        *http.Client
}

type DownloadProgress struct {
	Name       string
	Downloaded int64
	Total      int64
}

type DownloadSpec struct {
	Name        string
	URL         string
	Destination string
	ManualPath  string
}

type State struct {
	RootFSExists    bool
	UbuntuRelease   string
	PortMasterReady bool
	Mounted         bool
	RootFSArchive   string
	PortMasterZIP   string
}

func NewManager() (*Manager, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	client, err := newHTTPClient()
	if err != nil {
		return nil, err
	}
	return &Manager{
		AppDir:        filepath.Dir(executable),
		RootFS:        defaultRootFS,
		SDCard:        defaultSDCard,
		PortMasterApp: defaultPortMasterApp,
		Client:        client,
	}, nil
}

func newHTTPClient() (*http.Client, error) {
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(bundledRootCAs) {
		return nil, errors.New(`内置 CA 根证书包无效`)
	}
	return &http.Client{Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
		TLSClientConfig:       &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
	}}, nil
}

func (m *Manager) Status() State {
	state := State{}
	if data, err := os.ReadFile(filepath.Join(m.RootFS, `etc`, `os-release`)); err == nil {
		state.RootFSExists = true
		state.UbuntuRelease = osReleaseValue(string(data), `PRETTY_NAME`)
	}
	pmApp := m.portMasterApp()
	state.PortMasterReady = isDir(filepath.Join(pmApp, `PortMaster`))
	state.Mounted = hasMountAt(m.RootFS)
	state.RootFSArchive = m.findRootFSArchive()
	state.PortMasterZIP = m.findPortMasterZIP()
	return state
}

func (s State) Headline() string {
	if s.RootFSExists && s.PortMasterReady {
		return `环境已安装`
	}
	if s.RootFSExists {
		return `环境需要修复`
	}
	return `环境未安装`
}

func (s State) Description() string {
	parts := []string{}
	if s.RootFSExists {
		parts = append(parts, `Rootfs: `+fallback(s.UbuntuRelease, `已存在`))
	} else if s.RootFSArchive != `` {
		parts = append(parts, `Rootfs 包: `+s.RootFSArchive)
	} else {
		parts = append(parts, `未找到 Ubuntu 24.04 arm64 rootfs 包`)
	}
	parts = append(parts, fmt.Sprintf(`PortMaster: %s`, yesNo(s.PortMasterReady)))
	parts = append(parts, fmt.Sprintf(`当前挂载: %s`, yesNo(s.Mounted)))
	return strings.Join(parts, "\n")
}

func (m *Manager) MissingDownloads() []DownloadSpec {
	var missing []DownloadSpec
	if m.findRootFSArchive() == `` {
		missing = append(missing, DownloadSpec{
			Name:        `Ubuntu rootfs`,
			URL:         ubuntuRootfsURL,
			Destination: filepath.Join(m.SDCard, ubuntuArchiveName),
			ManualPath:  filepath.Join(m.SDCard, ubuntuArchiveName),
		})
	}
	if m.findPortMasterZIP() == `` {
		missing = append(missing, DownloadSpec{
			Name:        `PortMaster`,
			URL:         portMasterURL,
			Destination: filepath.Join(m.SDCard, `trimui.portmaster.zip`),
			ManualPath:  filepath.Join(m.SDCard, `trimui.portmaster.zip`),
		})
	}
	return missing
}

func (m *Manager) Install(setTitle func(string), logf func(string), progress func(DownloadProgress), autoDownload bool) error {
	logf(`清理现有 Ubuntu rootfs 和 PortMaster 程序`)
	if err := m.cleanInstalledEnvironment(logf, false); err != nil {
		return err
	}

	missing := m.MissingDownloads()
	if len(missing) > 0 && !autoDownload {
		return errors.New(`缺少安装包`)
	}
	for _, item := range missing {
		logf(`下载 ` + item.Name + `: ` + item.URL)
		if err := downloadFile(m.Client, item, progress); err != nil {
			return fmt.Errorf(`下载 %s: %w`, item.Name, err)
		}
		logf(`下载完成: ` + item.Destination)
	}

	state := m.Status()
	if !state.PortMasterReady {
		if state.PortMasterZIP == `` {
			return errors.New(`未找到 PortMaster；请把 trimui.portmaster.zip 放入 /mnt/SDCARD`)
		}
		setTitle(`解压 PortMaster 中`)
		logf(`解压 PortMaster: ` + state.PortMasterZIP)
		if err := installPortMasterZIP(state.PortMasterZIP, m.SDCard, m.portMasterApp()); err != nil {
			return fmt.Errorf(`解压 PortMaster: %w`, err)
		}
		if !isDir(filepath.Join(m.portMasterApp(), `PortMaster`)) {
			return errors.New(`PortMaster ZIP 目录结构错误：缺少 PortMaster 程序目录`)
		}
	}

	archive := state.RootFSArchive
	if !state.RootFSExists && archive == `` {
		return errors.New(`未找到 Ubuntu 24.04 arm64 rootfs 压缩包`)
	}

	setup := filepath.Join(m.AppDir, `scripts`, `trimui-ubuntu-chroot-setup.sh`)
	args := []string{setup, m.RootFS}
	if !state.RootFSExists {
		args = append(args, archive)
	}
	setTitle(`运行初始化脚本`)
	logf(`运行初始化脚本`)
	return runLogged(logf, `/bin/sh`, args, []string{`SDCARD=` + m.SDCard, `PM_APP=` + m.portMasterApp()})
}

func downloadFile(client *http.Client, spec DownloadSpec, progress func(DownloadProgress)) error {
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequest(http.MethodGet, spec.URL, nil)
	if err != nil {
		return err
	}
	request.Header.Set(`User-Agent`, `TrimUI-Ubuntu-PortMaster-Installer/1.0`)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf(`HTTP %s`, response.Status)
	}

	if err := os.MkdirAll(filepath.Dir(spec.Destination), 0755); err != nil {
		return err
	}
	partial := spec.Destination + `.part`
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		out.Close()
		if !committed {
			os.Remove(partial)
		}
	}()

	reporter := &progressWriter{
		name:     spec.Name,
		total:    response.ContentLength,
		callback: progress,
	}
	if progress != nil {
		progress(DownloadProgress{Name: spec.Name, Total: response.ContentLength})
	}
	if _, err := io.CopyBuffer(out, io.TeeReader(response.Body, reporter), make([]byte, 128*1024)); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(partial, spec.Destination); err != nil {
		return err
	}
	committed = true
	if progress != nil {
		progress(DownloadProgress{Name: spec.Name, Downloaded: reporter.downloaded, Total: response.ContentLength})
	}
	return nil
}

type progressWriter struct {
	name       string
	total      int64
	downloaded int64
	lastBytes  int64
	lastPct    int64
	callback   func(DownloadProgress)
}

func (w *progressWriter) Write(data []byte) (int, error) {
	w.downloaded += int64(len(data))
	if w.callback == nil {
		return len(data), nil
	}
	shouldReport := false
	if w.total > 0 {
		percent := w.downloaded * 100 / w.total
		if percent != w.lastPct {
			w.lastPct = percent
			shouldReport = true
		}
	} else if w.downloaded-w.lastBytes >= 1024*1024 {
		shouldReport = true
	}
	if shouldReport {
		w.lastBytes = w.downloaded
		w.callback(DownloadProgress{Name: w.name, Downloaded: w.downloaded, Total: w.total})
	}
	return len(data), nil
}

func (m *Manager) Uninstall(logf func(string)) error {
	return m.cleanInstalledEnvironment(logf, true)
}

func (m *Manager) cleanInstalledEnvironment(logf func(string), removeArchives bool) error {
	unmount := filepath.Join(m.AppDir, `scripts`, `trimui-ubuntu-chroot-unmount.sh`)
	if isDir(m.RootFS) {
		logf(`卸载 chroot 挂载`)
		if err := runLogged(logf, `/bin/sh`, []string{unmount, m.RootFS}, nil); err != nil {
			return err
		}
	}
	if hasMountAt(m.RootFS) {
		return errors.New(`仍有进程占用 chroot；请退出 PortMaster 和所有 port 游戏后重试`)
	}

	logf(`彻底删除 Ubuntu rootfs: ` + m.RootFS)
	if err := removeInstallTree(m.RootFS, filepath.Join(`/mnt`, `UDISK`), defaultRootFS); err != nil {
		return fmt.Errorf(`删除 rootfs: %w`, err)
	}

	pmApp := m.portMasterApp()
	logf(`彻底删除 PortMaster: ` + pmApp)
	if err := removeInstallTree(pmApp, filepath.Join(defaultSDCard, `Apps`), defaultPortMasterApp); err != nil {
		return fmt.Errorf(`删除 PortMaster: %w`, err)
	}
	if removeArchives {
		for _, archive := range []string{
			filepath.Join(m.SDCard, ubuntuArchiveName),
			filepath.Join(m.SDCard, `trimui.portmaster.zip`),
		} {
			if err := os.Remove(archive); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf(`删除下载缓存 %s: %w`, archive, err)
			}
			logf(`删除下载缓存: ` + archive)
		}
	}

	logf(`游戏和存档目录已保留: ` + filepath.Join(m.SDCard, `Data`, `ports`))
	return nil
}

func removeInstallTree(target, expectedParent, expectedTarget string) error {
	target = filepath.Clean(target)
	expectedParent = filepath.Clean(expectedParent)
	expectedTarget = filepath.Clean(expectedTarget)
	if target != expectedTarget || filepath.Dir(target) != expectedParent || target == `/` || target == `.` {
		return fmt.Errorf(`拒绝删除非预期目录: %s`, target)
	}
	return os.RemoveAll(target)
}

func (m *Manager) findRootFSArchive() string {
	return firstGlob([]string{filepath.Join(m.SDCard, `ubuntu-base-*.tar.*`)})
}

func (m *Manager) findPortMasterZIP() string {
	return firstExisting([]string{filepath.Join(m.SDCard, `trimui.portmaster.zip`)})
}

func (m *Manager) portMasterApp() string {
	if m.PortMasterApp != `` {
		return m.PortMasterApp
	}
	return filepath.Join(m.SDCard, `Apps`, portMasterAppDirName)
}

func installPortMasterZIP(source, sdcard, destination string) error {
	if err := os.MkdirAll(sdcard, 0755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(sdcard, `.ubuntu-portmaster-extract-`)
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := extractZIP(source, staging); err != nil {
		return err
	}
	officialApp := filepath.Join(staging, `Apps`, `PortMaster`)
	if !isDir(filepath.Join(officialApp, `PortMaster`)) {
		return errors.New(`官方 ZIP 中缺少 Apps/PortMaster/PortMaster`)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf(`目标目录已经存在: %s`, destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(officialApp, destination)
}

func runLogged(logf func(string), program string, args, extraEnv []string) error {
	cmd := exec.Command(program, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		logf(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf(`%s: %w`, filepath.Base(program), err)
	}
	return nil
}

func extractZIP(source, destination string) error {
	zr, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer zr.Close()
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	for _, file := range zr.File {
		target, err := archiveTarget(root, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
		if err == nil {
			_, err = io.Copy(out, rc)
		}
		closeErr := rc.Close()
		if out != nil {
			if e := out.Close(); err == nil {
				err = e
			}
		}
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func archiveTarget(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == `.` || filepath.IsAbs(clean) || clean == `..` || strings.HasPrefix(clean, `..`+string(os.PathSeparator)) {
		return ``, fmt.Errorf(`不安全的 ZIP 路径: %q`, name)
	}
	target := filepath.Join(root, clean)
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return ``, fmt.Errorf(`ZIP 路径越界: %q`, name)
	}
	return target, nil
}

func hasMountAt(root string) bool {
	data, err := os.ReadFile(`/proc/mounts`)
	if err != nil {
		return false
	}
	root = filepath.Clean(root)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 {
			target := strings.ReplaceAll(fields[1], `\040`, ` `)
			if target == root || strings.HasPrefix(target, root+string(os.PathSeparator)) {
				return true
			}
		}
	}
	return false
}

func osReleaseValue(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		name, value, found := strings.Cut(line, `=`)
		if found && name == key {
			return strings.Trim(value, `"'`)
		}
	}
	return ``
}

func firstGlob(patterns []string) string {
	var matches []string
	for _, pattern := range patterns {
		found, _ := filepath.Glob(pattern)
		matches = append(matches, found...)
	}
	sort.Strings(matches)
	if len(matches) > 0 {
		return matches[0]
	}
	return ``
}

func firstExisting(paths []string) string {
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ``
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	temporary := destination + `.tmp`
	out, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(temporary)
		return closeErr
	}
	return os.Rename(temporary, destination)
}

func isDir(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }
func yesNo(value bool) string {
	if value {
		return `是`
	}
	return `否`
}
func fallback(value, other string) string {
	if value != `` {
		return value
	}
	return other
}
