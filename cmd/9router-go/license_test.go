package main

import (
	"os"
	"testing"
	"time"

)

func TestLicenseCommandShape(t *testing.T) {
	cmd := licenseCommand()
	if cmd.Name != "license" {
		t.Fatalf("command name = %q", cmd.Name)
	}

	want := map[string]bool{
		"activate": false,
		"status":   false,
		"renew":    false,
		"menu":     false,
	}
	for _, sub := range cmd.Subcommands {
		if _, ok := want[sub.Name]; ok {
			want[sub.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("missing license subcommand %q", name)
		}
	}
}

func TestCurrentLicenseBuildIdentityRejectsBetaWithoutBuildID(t *testing.T) {
	oldChannel := licenseBuildChannel
	oldID := licenseBuildID
	oldDeadline := licenseProCapableUntil
	t.Cleanup(func() {
		licenseBuildChannel = oldChannel
		licenseBuildID = oldID
		licenseProCapableUntil = oldDeadline
	})

	licenseBuildChannel = "beta"
	licenseBuildID = ""
	licenseProCapableUntil = ""

	if _, err := currentLicenseBuildIdentity(); err == nil {
		t.Fatal("beta build without build id unexpectedly accepted")
	}
}

func TestCurrentLicenseBuildIdentityAllowsDevWithoutBuildID(t *testing.T) {
	oldChannel := licenseBuildChannel
	oldID := licenseBuildID
	oldDeadline := licenseProCapableUntil
	t.Cleanup(func() {
		licenseBuildChannel = oldChannel
		licenseBuildID = oldID
		licenseProCapableUntil = oldDeadline
	})

	licenseBuildChannel = "dev"
	licenseBuildID = ""
	licenseProCapableUntil = ""

	build, err := currentLicenseBuildIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if build.Channel != "dev" || build.ID != "" {
		t.Fatalf("unexpected dev build identity: %+v", build)
	}
}

func TestCurrentLicenseBuildIdentityParsesHardExpiry(t *testing.T) {
	oldChannel := licenseBuildChannel
	oldID := licenseBuildID
	oldDeadline := licenseProCapableUntil
	t.Cleanup(func() {
		licenseBuildChannel = oldChannel
		licenseBuildID = oldID
		licenseProCapableUntil = oldDeadline
	})

	licenseBuildChannel = "beta"
	licenseBuildID = "beta-build-7"
	licenseProCapableUntil = "2026-11-15T00:00:00Z"

	build, err := currentLicenseBuildIdentity()
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	if !build.ProCapableUntil.Equal(want) {
		t.Fatalf("deadline = %s, want %s", build.ProCapableUntil, want)
	}
	if build.ID != "beta-build-7" {
		t.Fatalf("build id = %q", build.ID)
	}
}

func TestCurrentLicenseControlPlaneURLAllowsExplicitOverride(t *testing.T) {
	const override = "https://license.example.test"
	old, had := os.LookupEnv("NINEROUTER_LICENSE_URL")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("NINEROUTER_LICENSE_URL", old)
		} else {
			_ = os.Unsetenv("NINEROUTER_LICENSE_URL")
		}
	})
	if err := os.Setenv("NINEROUTER_LICENSE_URL", override); err != nil {
		t.Fatal(err)
	}
	if got := currentLicenseControlPlaneURL(); got != override {
		t.Fatalf("control plane = %q, want %q", got, override)
	}
}
