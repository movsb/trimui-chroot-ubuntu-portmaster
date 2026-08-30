package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/movsb/fbiw"
)

//go:embed port_launch.html
var portLaunchAssets embed.FS

type portMetadata struct {
	Items []string `json:"items"`
	Attr  struct {
		Title           string `json:"title"`
		ReadyToRun      *bool  `json:"rtr"`
		Install         string `json:"inst"`
		InstallMarkdown string `json:"inst_md"`
	} `json:"attr"`
}

func launchPort(manager *Manager, script string) error {
	script, err := validatePortScript(manager.SDCard, script)
	if err != nil {
		return err
	}
	metadata, err := findPortMetadata(manager.SDCard, script)
	if err != nil {
		return err
	}
	if metadata == nil || metadata.Attr.ReadyToRun == nil || *metadata.Attr.ReadyToRun {
		return runPort(manager, script)
	}

	instructions := strings.TrimSpace(metadata.Attr.Install)
	if instructions == "" {
		instructions = strings.TrimSpace(metadata.Attr.InstallMarkdown)
	}
	if instructions == "" {
		instructions = "请在 PortMaster 中打开 Show Info，按说明复制正版游戏文件。"
	}
	name := strings.TrimSpace(metadata.Attr.Title)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(script), filepath.Ext(script))
	}

	app := fbiw.NewApp(fbiw.WithSystemFont(os.DirFS(`/usr/trimui/res`), `full.ttf`))
	defer app.Close()
	opener := app.NewDesktop(portLaunchAssets, `port_launch.html`)
	shouldLaunch := false
	app.ShowAlertDialog(opener, fbiw.AlertDialogOptions{
		Title:       `需要游戏本体`,
		Description: name + " 不包含游戏本体。\n\nPortMaster 只安装了移植程序和启动脚本；未补齐所需的正版游戏文件时，启动后通常会立即退出。\n\n安装说明：\n" + instructions,
		ActionText:  `仍然启动`,
		CancelText:  `取消`,
		OnAction: func() {
			shouldLaunch = true
			app.Quit()
		},
		OnCancel: app.Quit,
	})
	app.Run()
	if !shouldLaunch {
		return nil
	}
	return runPort(manager, script)
}

func validatePortScript(sdCard, script string) (string, error) {
	root := filepath.Clean(filepath.Join(sdCard, `Data`, `ports`))
	script = filepath.Clean(script)
	hostPath := hostPortScriptPath(root, script)
	relative, err := filepath.Rel(root, hostPath)
	if err != nil || relative == `..` || strings.HasPrefix(relative, `..`+string(filepath.Separator)) {
		return "", errors.New(`游戏启动脚本不在 PortMaster 数据目录中`)
	}
	if !strings.EqualFold(filepath.Ext(script), `.sh`) {
		return "", errors.New(`PortMaster 游戏启动脚本必须是 .sh 文件`)
	}
	if stat, err := os.Stat(hostPath); err != nil || stat.IsDir() {
		return "", fmt.Errorf(`找不到游戏启动脚本: %s`, script)
	}
	return script, nil
}

func findPortMetadata(sdCard, script string) (*portMetadata, error) {
	root := filepath.Join(sdCard, `Data`, `ports`)
	script = hostPortScriptPath(root, script)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), `port.json`)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var metadata portMetadata
		if err := json.Unmarshal(data, &metadata); err != nil {
			return nil, fmt.Errorf(`解析 %s: %w`, path, err)
		}
		for _, item := range metadata.Items {
			if filepath.Clean(filepath.Join(root, filepath.FromSlash(item))) == script {
				return &metadata, nil
			}
		}
	}
	return nil, nil
}

func hostPortScriptPath(root, script string) string {
	const chrootPorts = `/roms/ports`
	script = filepath.Clean(script)
	if script == chrootPorts {
		return filepath.Clean(root)
	}
	if after, ok := strings.CutPrefix(script, chrootPorts+string(filepath.Separator)); ok {
		return filepath.Join(root, after)
	}
	return script
}

func runPort(manager *Manager, script string) error {
	launcher := filepath.Join(manager.PortMasterApp, `launch.chroot.sh`)
	command := exec.Command(launcher, `/bin/bash`, script)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}
