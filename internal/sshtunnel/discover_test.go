package sshtunnel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverReadsConfigAndKnownHosts(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	_ = os.MkdirAll(filepath.Join(ssh, "conf.d"), 0o700)
	_ = os.WriteFile(filepath.Join(ssh, "config"), []byte(`
# servers
Include conf.d/*
Host prod web-1
    HostName 10.0.0.5
    User deploy
    Port 2222
    IdentityFile ~/.ssh/prod_key

Host staging
  HostName=staging.example.com

Host github.com
  User git

Host *.internal !bastion
  User ops

Match host foo
  User nobody

Host *
  User kayra
  IdentityFile ~/.ssh/id_ed25519
`), 0o600)
	_ = os.WriteFile(filepath.Join(ssh, "conf.d", "lab"), []byte("Host lab\n  HostName 192.168.1.20\n"), 0o600)
	_ = os.WriteFile(filepath.Join(ssh, "known_hosts"), []byte(`10.0.0.5 ssh-ed25519 AAAA
203.0.113.9,203.0.113.10 ssh-ed25519 AAAA
[198.51.100.7]:2200 ecdsa-sha2-nistp256 AAAA
|1|hashed=|salt= ssh-ed25519 AAAA
github.com ssh-ed25519 AAAA
localhost ssh-ed25519 AAAA
@cert-authority *.example.com ssh-ed25519 AAAA
`), 0o600)

	got := Discover(home)
	by := map[string]Discovered{}
	for _, d := range got {
		key := d.Alias
		if key == "" {
			key = d.HostName
		}
		by[key] = d
	}
	prod := by["prod"]
	if prod.HostName != "10.0.0.5" || prod.User != "deploy" || prod.Port != 2222 || prod.KeyPath != "~/.ssh/prod_key" || prod.Source != "config" {
		t.Fatalf("prod = %+v", prod)
	}
	if by["web-1"].HostName != "10.0.0.5" {
		t.Fatalf("second name of the same block: %+v", by["web-1"])
	}
	// "Host *" fills in what an entry does not set
	if st := by["staging"]; st.HostName != "staging.example.com" || st.User != "kayra" || st.KeyPath != "~/.ssh/id_ed25519" {
		t.Fatalf("staging = %+v", st)
	}
	if by["lab"].HostName != "192.168.1.20" {
		t.Fatalf("included file not read: %+v", got)
	}
	// known_hosts adds what the config does not name, with its port
	if d := by["198.51.100.7"]; d.Port != 2200 || d.Source != "known_hosts" {
		t.Fatalf("known host with port = %+v", d)
	}
	if _, ok := by["203.0.113.10"]; !ok {
		t.Fatal("second name on a known_hosts line missing")
	}
	// left out: forges, wildcards, hashed, local, a host the config already names
	for _, k := range []string{"github.com", "*.internal", "localhost", "10.0.0.5"} {
		if _, ok := by[k]; ok {
			t.Fatalf("%s should be left out: %+v", k, got)
		}
	}
	if len(Discover(t.TempDir())) != 0 {
		t.Fatal("no ~/.ssh should find nothing")
	}
}
