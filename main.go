package main

import (
	"embed"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/movsb/fbiw"
)

//go:embed main.html
var uiAssets embed.FS

type window struct {
	app     *fbiw.App
	doc     *fbiw.Document
	manager *Manager

	status    *fbiw.Text   `css:"#status"`
	details   *fbiw.Text   `css:"#details"`
	log       *fbiw.Text   `css:"#log"`
	install   *fbiw.Button `css:"#install"`
	uninstall *fbiw.Button `css:"#uninstall"`

	buttons  []*fbiw.Button
	selected int
	busy     bool
	mu       sync.Mutex
	lines    []string
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--normalize-icon" {
		if err := normalizeIcon(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	manager, err := NewManager()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if len(os.Args) > 1 && os.Args[1] == "--status" {
		status := manager.Status().Description() + "\n"
		// fbiw redirects non-TTY stdout to /tmp/fbiw.log during package init.
		// Keep a deterministic status artifact for SSH and automated diagnostics.
		_ = os.WriteFile(`/tmp/ubuntu-portmaster-status.txt`, []byte(status), 0644)
		fmt.Print(status)
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "--launch-port" {
		if err := launchPort(manager, os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	app := fbiw.NewApp(fbiw.WithSystemFont(os.DirFS(`/usr/trimui/res`), `full.ttf`))
	defer app.Close()

	w := &window{
		app:     app,
		doc:     app.NewDesktop(uiAssets, `main.html`),
		manager: manager,
	}
	w.doc.Bind(w)
	w.buttons = []*fbiw.Button{w.install, w.uninstall}
	w.install.OnClick(w.prepareInstall)
	w.uninstall.OnClick(w.confirmUninstall)
	w.doc.Listen(fbiw.StickDownEvent, w.handleKeys)
	w.selectButton(0)
	w.refresh()
	app.Run()
}

func (w *window) prepareInstall() {
	if w.busy {
		return
	}
	missing := w.manager.MissingDownloads()
	if len(missing) == 0 {
		w.confirmFreshInstall(false, ``)
		return
	}
	var sections []string
	for _, item := range missing {
		sections = append(sections,
			item.Name+` 未找到。`+"\n"+
				`可自行下载到：`+item.ManualPath+"\n"+
				`或者自动下载：`+item.URL)
	}
	w.confirmFreshInstall(true, strings.Join(sections, "\n\n"))
}

func (w *window) confirmFreshInstall(autoDownload bool, missingDescription string) {
	description := `将先彻底清理历史安装（如果有的话），再执行全量安装。原始已安装的游戏与存档不会动，可放心操作。`
	if missingDescription != `` {
		description += "\n\n" + missingDescription + "\n\n按 A 清理并自动下载，按 B 取消。"
	}
	action := `清理并全量安装`
	if autoDownload {
		action = `清理、下载并安装`
	}
	w.app.ShowAlertDialog(w.doc, fbiw.AlertDialogOptions{
		Title:         `全量重新安装？`,
		Description:   description,
		ActionText:    action,
		ActionVariant: fbiw.ButtonDestructive,
		CancelText:    `取消`,
		OnAction:      func() { w.start(`install`, autoDownload) },
	})
}

func (w *window) setStatusTimeout(s string, d time.Duration) {
	backup := w.status.GetText()
	w.status.SetText(s)
	w.doc.SetTimeout(int(d.Milliseconds()), func() {
		if w.status.GetText() == s {
			w.status.SetText(backup)
		}
	})
}

func (w *window) handleKeys(event *fbiw.Event) {
	if event.Stick.Repeat {
		return
	}
	if event.Stick.Name == fbiw.B {
		if w.busy {
			w.setStatusTimeout(`操作进行中，完成后按 B 退出`, time.Second)
			return
		}
		w.app.Quit()
		return
	}
	if w.busy {
		return
	}
	switch event.Stick.Name {
	case fbiw.Left:
		w.selectButton((w.selected - 1 + len(w.buttons)) % len(w.buttons))
	case fbiw.Right:
		w.selectButton((w.selected + 1) % len(w.buttons))
	}
}

func (w *window) selectButton(index int) {
	if len(w.buttons) == 0 {
		return
	}
	w.buttons[w.selected].ClassRemove(`selected`)
	w.selected = index
	w.buttons[w.selected].ClassAdd(`selected`)
	w.buttons[w.selected].Activate()
}

func (w *window) refresh() {
	state := w.manager.Status()
	w.status.SetText(state.Headline())
	w.details.SetText(state.Description())
	hasInstalledFiles := state.RootFSExists || state.PortMasterReady || state.RootFSArchive != `` || state.PortMasterZIP != ``
	w.uninstall.SetDisabled(!hasInstalledFiles || w.busy)
	w.install.SetDisabled(w.busy)
}

func (w *window) confirmUninstall() {
	if w.busy {
		return
	}
	w.app.ShowAlertDialog(w.doc, fbiw.AlertDialogOptions{
		Title:         `卸载Port环境？`,
		Description:   "将彻底清理由本安装器产生的各种程序、文件系统和已下载的安装包，无法恢复。\n\n已安装的游戏和存档不会删除。",
		ActionText:    `彻底删除`,
		ActionVariant: fbiw.ButtonDestructive,
		CancelText:    `取消`,
		OnAction:      func() { w.start(`uninstall`, false) },
	})
}

func (w *window) setStatusAsync(t string) {
	w.app.Async(func() {
		w.status.SetText(t)
	})
}

func (w *window) start(operation string, autoDownload bool) {
	if w.busy {
		return
	}
	w.busy = true
	w.lines = nil
	w.refresh()
	w.status.SetText(map[string]string{`install`: `正在清理并全量安装…`, `uninstall`: `正在卸载…`}[operation])

	go func() {
		var err error
		switch operation {
		case `install`:
			err = w.manager.Install(w.setStatusAsync, w.appendLog, w.showDownloadProgress, autoDownload)
		case `uninstall`:
			err = w.manager.Uninstall(w.appendLog)
		}
		w.app.Async(func() {
			w.busy = false
			if err != nil {
				w.appendLog(`失败：` + err.Error())
				w.status.SetText(`操作失败`)
			} else {
				w.appendLog(`操作完成`)
			}
			w.refresh()
		})
	}()
}

func (w *window) showDownloadProgress(progress DownloadProgress) {
	w.app.Async(func() {
		if progress.Total > 0 {
			percent := progress.Downloaded * 100 / progress.Total
			w.status.SetText(fmt.Sprintf(`正在下载 %s：%d%%（%.1f / %.1f MiB）`,
				progress.Name, percent, mib(progress.Downloaded), mib(progress.Total)))
		} else {
			w.status.SetText(fmt.Sprintf(`正在下载 %s：%.1f MiB`, progress.Name, mib(progress.Downloaded)))
		}
	})
}

func mib(bytes int64) float64 {
	return float64(bytes) / (1024 * 1024)
}

func (w *window) appendLog(line string) {
	line = strings.TrimSpace(line)
	if line == `` {
		return
	}
	w.app.Async(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.lines = append(w.lines, line)
		if len(w.lines) > 6 {
			w.lines = w.lines[len(w.lines)-6:]
		}
		w.log.SetText(strings.Join(w.lines, "\n"))
	})
}
