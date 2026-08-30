package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindPortMetadata(t *testing.T) {
	sdCard := t.TempDir()
	ports := filepath.Join(sdCard, `Data`, `ports`)
	game := filepath.Join(ports, `limbo`)
	if err := os.MkdirAll(game, 0755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(ports, `Limbo.sh`)
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(game, `port.json`), []byte(`{
  "items": ["Limbo.sh", "limbo"],
  "attr": {"title": "LIMBO", "rtr": false, "inst": "Copy game data."}
}`), 0644); err != nil {
		t.Fatal(err)
	}

	metadata, err := findPortMetadata(sdCard, script)
	if err != nil {
		t.Fatal(err)
	}
	if metadata == nil || metadata.Attr.ReadyToRun == nil || *metadata.Attr.ReadyToRun {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	if metadata.Attr.Install != `Copy game data.` {
		t.Fatalf("install = %q", metadata.Attr.Install)
	}
}

func TestValidatePortScriptRejectsOutsidePath(t *testing.T) {
	if _, err := validatePortScript(t.TempDir(), `/tmp/not-a-port.sh`); err == nil {
		t.Fatal(`expected path validation error`)
	}
}

func TestChrootPortScriptPathMapsToSDCard(t *testing.T) {
	sdCard := t.TempDir()
	ports := filepath.Join(sdCard, `Data`, `ports`)
	if err := os.MkdirAll(ports, 0755); err != nil {
		t.Fatal(err)
	}
	hostScript := filepath.Join(ports, `Limbo.sh`)
	if err := os.WriteFile(hostScript, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := validatePortScript(sdCard, `/roms/ports/Limbo.sh`)
	if err != nil {
		t.Fatal(err)
	}
	if got != `/roms/ports/Limbo.sh` {
		t.Fatalf("script = %q", got)
	}
}
